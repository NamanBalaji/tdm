package torrent

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	anacrolix "github.com/anacrolix/torrent"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/NamanBalaji/tdm/internal/download"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

const partFileExt = ".part"

// ErrUnsafeRemovePath is returned when a download's filename would resolve
// outside its download directory.
var ErrUnsafeRemovePath = errors.New("refusing to remove path outside download dir")

// ErrHandleClosed is returned when the torrent handle is dropped from the
// client while the download is still running.
var ErrHandleClosed = errors.New("torrent handle was closed")

// torrentState is the protocol-specific state persisted in Download.State.
type torrentState struct {
	InfoHash string `json:"infoHash"`
	IsMagnet bool   `json:"isMagnet"`
	Name     string `json:"name"`
	// Metainfo holds the bencoded torrent metadata so resume never depends
	// on the origin URL (which may be dead) or a fresh DHT lookup.
	Metainfo []byte `json:"metainfo,omitempty"`
}

type Downloader struct {
	client *torrentPkg.Client
	cfg    *config.TorrentConfig
}

func New(client *torrentPkg.Client, cfg *config.TorrentConfig) *Downloader {
	return &Downloader{client: client, cfg: cfg}
}

func (d *Downloader) Type() string { return "torrent" }

func (d *Downloader) CanHandle(url string) bool {
	return torrentPkg.HasTorrentFile(url) || torrentPkg.IsValidMagnetLink(url)
}

func (d *Downloader) Init(ctx context.Context, url string, priority int) (*download.Download, error) {
	isMagnet := torrentPkg.IsValidMagnetLink(url)

	// Fetch metadata — this may take time for magnet links
	th, isNew, err := d.client.GetTorrentHandler(ctx, url, isMagnet)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent metadata: %w", err)
	}

	// Only drop a handle this call attached: the same infohash may already
	// be attached and actively seeding/downloading, and dropping that
	// handle would silently kill its transfer.
	if isNew {
		defer th.Drop()
	}

	st := torrentState{
		InfoHash: th.InfoHash().HexString(),
		IsMagnet: isMagnet,
		Name:     th.Name(),
		Metainfo: encodeMetainfo(th),
	}

	stateBytes, err := json.Marshal(&st)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &download.Download{
		ID:        uuid.New(),
		URL:       url,
		Filename:  th.Name(),
		Dir:       d.cfg.DownloadDir,
		Status:    download.Pending,
		Priority:  priority,
		Type:      "torrent",
		TotalSize: th.Length(),
		State:     stateBytes,
	}, nil
}

func (d *Downloader) Start(ctx context.Context, dl *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
	var st torrentState
	if err := json.Unmarshal(dl.State, &st); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	th, ownsHandle, err := d.getHandle(ctx, dl.URL, &st)
	if err != nil {
		return fmt.Errorf("failed to get torrent handler: %w", err)
	}

	// drop releases the handle, but only if this Start attached it —
	// AddTorrentSpec returns the SAME handle for an already-present
	// infohash, and dropping a handle another download (or a seeding
	// session) owns would silently kill its transfer.
	drop := func() {
		if ownsHandle {
			th.Drop()
		}
	}

	// No data verification here: the piece-completion DB in the data dir
	// already tracks what's downloaded, so resume/preemption doesn't
	// re-hash the whole torrent. Verification belongs to an explicit
	// "force recheck" action.
	th.DownloadAll()

	// Poll loop: check progress and completion every 500ms
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			drop()
			return ctx.Err()

		case <-th.Closed():
			// The handle was dropped elsewhere (e.g. the record was removed
			// mid-run); without this the loop would poll a dead torrent
			// forever with frozen progress. When our own ctx was cancelled
			// too (remove cancels first, then drops), report the
			// cancellation — not a spurious error for a deliberate removal.
			if err := ctx.Err(); err != nil {
				return err
			}

			return ErrHandleClosed

		case <-ticker.C:
			info := th.Info()
			if info == nil {
				continue
			}

			downloaded := th.BytesCompleted()
			totalSize := th.Length()
			onProgress(downloaded, totalSize, nil)

			// Check completion. (Legacy anacrolix .part leftovers from the
			// part-file era are NOT cleaned here: in a shared download dir
			// a same-named "<name>.part" may belong to someone else — they
			// are only removed with the download itself, in Remove.)
			if th.Complete().Bool() && downloaded >= totalSize {
				// When seeding is enabled, keep the torrent attached so it
				// keeps uploading for the rest of this session.
				if !d.cfg.Seed {
					drop()
				}

				return nil
			}
		}
	}
}

func (d *Downloader) Remove(dl *download.Download) error {
	// Stop any transfer (including background seeding) before deleting files.
	var st torrentState
	if err := json.Unmarshal(dl.State, &st); err == nil && st.InfoHash != "" {
		d.client.DropTorrent(st.InfoHash)
	}

	downloadPath := filepath.Join(dl.Dir, dl.Filename)

	// The filename comes from untrusted torrent metadata; refuse to delete
	// anything that escapes the download directory.
	rel, err := filepath.Rel(dl.Dir, downloadPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == "." {
		return fmt.Errorf("%w: %q", ErrUnsafeRemovePath, dl.Filename)
	}

	if err := os.RemoveAll(downloadPath); err != nil {
		return fmt.Errorf("remove download: %w", err)
	}

	if err := os.RemoveAll(downloadPath + partFileExt); err != nil {
		return fmt.Errorf("remove part file: %w", err)
	}

	slog.Debug("removed torrent files", "filename", dl.Filename)

	return nil
}

// getHandle re-adds the torrent to the client, preferring the metainfo
// persisted at Init so resume never needs the origin URL. isNew reports
// whether this call attached the handle (and therefore owns it).
func (d *Downloader) getHandle(ctx context.Context, url string, st *torrentState) (*anacrolix.Torrent, bool, error) {
	if len(st.Metainfo) > 0 {
		th, isNew, err := d.client.AddTorrentFromBytes(ctx, st.Metainfo)
		if err == nil {
			return th, isNew, nil
		}

		slog.Warn("failed to add torrent from persisted metainfo, falling back to URL", "err", err)
	}

	return d.client.GetTorrentHandler(ctx, url, st.IsMagnet)
}

// encodeMetainfo bencodes the torrent's metainfo for persistence. Returns
// nil if encoding fails (resume then falls back to the origin URL).
func encodeMetainfo(th *anacrolix.Torrent) []byte {
	mi := th.Metainfo()

	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		slog.Warn("failed to encode torrent metainfo", "err", err)
		return nil
	}

	return buf.Bytes()
}
