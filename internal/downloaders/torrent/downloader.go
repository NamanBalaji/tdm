package torrent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/logger"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

const partFileExt = ".part"

// torrentState is the protocol-specific state persisted in Download.BackendState.
type torrentState struct {
	InfoHash string `json:"infoHash"`
	IsMagnet bool   `json:"isMagnet"`
	Name     string `json:"name"`
}

type Downloader struct {
	client *torrentPkg.Client
	dir    string
}

func New(client *torrentPkg.Client, dir string) *Downloader {
	return &Downloader{client: client, dir: dir}
}

func (d *Downloader) Type() string { return "torrent" }

func (d *Downloader) CanHandle(url string) bool {
	return torrentPkg.HasTorrentFile(url) || torrentPkg.IsValidMagnetLink(url)
}

func (d *Downloader) Init(ctx context.Context, url string, priority int) (*download.Download, error) {
	isMagnet := torrentPkg.IsValidMagnetLink(url)

	// Fetch metadata — this may take time for magnet links
	th, err := d.client.GetTorrentHandler(ctx, url, isMagnet)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent metadata: %w", err)
	}
	defer th.Drop()

	st := torrentState{
		InfoHash: th.InfoHash().HexString(),
		IsMagnet: isMagnet,
		Name:     th.Name(),
	}

	stateBytes, err := json.Marshal(&st)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &download.Download{
		ID:        uuid.New(),
		URL:       url,
		Filename:  th.Name(),
		Dir:       d.dir,
		Status:    download.Pending,
		Priority:  priority,
		Type:      "torrent",
		TotalSize: th.Length(),
		State:     stateBytes,
	}, nil
}

func (d *Downloader) Start(ctx context.Context, dl *download.Download, onProgress func(int64, int64)) error {
	var st torrentState
	if err := json.Unmarshal(dl.State, &st); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Get torrent handler (re-adds torrent to client)
	th, err := d.client.GetTorrentHandler(ctx, dl.URL, st.IsMagnet)
	if err != nil {
		if th != nil {
			th.Drop()
		}

		return fmt.Errorf("failed to get torrent handler: %w", err)
	}

	// Verify existing data and start downloading
	if err := th.VerifyDataContext(ctx); err != nil {
		th.Drop()
		return fmt.Errorf("failed to verify torrent data: %w", err)
	}

	th.DownloadAll()

	// Poll loop: check progress and completion every 500ms
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			th.Drop()
			return ctx.Err()

		case <-ticker.C:
			info := th.Info()
			if info == nil {
				continue
			}

			downloaded := th.BytesCompleted()
			totalSize := th.Length()
			onProgress(downloaded, totalSize)

			// Check completion
			if th.Complete().Bool() && downloaded >= totalSize {
				th.Drop()
				return nil
			}
		}
	}
}

func (d *Downloader) Remove(dl *download.Download) error {
	downloadPath := filepath.Join(dl.Dir, dl.Filename)

	if err := os.RemoveAll(downloadPath); err != nil {
		return fmt.Errorf("remove download: %w", err)
	}

	if err := os.RemoveAll(downloadPath + partFileExt); err != nil {
		return fmt.Errorf("remove part file: %w", err)
	}

	logger.Debugf("removed torrent files for %s", dl.Filename)

	return nil
}
