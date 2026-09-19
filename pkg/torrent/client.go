package torrent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	g "github.com/anacrolix/generics"
	analog "github.com/anacrolix/log"

	"github.com/NamanBalaji/tdm/internal/config"
)

var (
	ErrNilClient       = errors.New("client is nil")
	ErrNilMetainfo     = errors.New("metainfo is nil")
	ErrMetadataTimeout = errors.New("timeout waiting for metadata")
	ErrMetainfoFetch   = errors.New("failed to fetch metainfo")
)

// Client wraps the anacrolix torrent client with thread-safe operations.
type Client struct {
	timeout time.Duration
	client  atomic.Pointer[torrent.Client]
	config  *torrent.ClientConfig
}

// NewClient creates a new torrent client with optimized configuration.
func NewClient(cfg *config.TorrentConfig) (*Client, error) {
	analog.Default.SetHandlers(analog.DiscardHandler)

	config := torrent.NewDefaultClientConfig()

	config.DataDir = cfg.DownloadDir
	config.Seed = cfg.Seed

	// CRITICAL FIX: Disable UTP to prevent memory leaks
	// See: https://github.com/anacrolix/torrent/issues/392
	config.DisableUTP = true

	config.EstablishedConnsPerTorrent = cfg.EstablishedConnectionsPerTorrent
	config.HalfOpenConnsPerTorrent = cfg.HalfOpenConnectionsPerTorrent
	config.TotalHalfOpenConns = cfg.TotalHalfOpenConnections

	config.NoDHT = cfg.DisableDHT      // Enable DHT
	config.DisablePEX = cfg.DisablePEX // Enable Peer Exchange
	config.DisableTrackers = cfg.DisableTrackers
	config.DisableIPv6 = cfg.DisableIPv6

	// Part files must stay disabled: with them enabled, every storage open
	// (each re-add after pause/preemption, and each app start) resets the
	// persistent piece-completion DB to not-complete for any file still at
	// its .part name (setCompletionFromPartFiles), which would silently
	// re-download all partial progress. With part files off, the bolt
	// completion DB in the data dir is the durable resume record — no
	// re-hash needed on resume. (One-time upgrade cost: data in legacy
	// .part files is not read back; those torrents re-download.)
	//
	// Torrent bookkeeping problems must not brick the whole app (HTTP
	// downloads don't need any of this) — degrade to in-memory completion,
	// which merely costs re-downloads on restart.
	if err := os.MkdirAll(config.DataDir, 0o755); err != nil {
		slog.Warn("failed to create torrent data dir, torrent resume may be degraded", "dir", config.DataDir, "err", err)
	}

	completion, err := storage.NewDefaultPieceCompletionForDir(config.DataDir)
	if err != nil {
		slog.Warn("failed to open piece completion db, falling back to in-memory completion", "err", err)

		completion = storage.NewMapPieceCompletion()
	}

	config.DefaultStorage = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   config.DataDir,
		PieceCompletion: completion,
		UsePartFiles:    g.Some(false),
	})

	client, err := torrent.NewClient(config)
	if err != nil {
		return nil, err
	}

	c := &Client{
		config:  config,
		timeout: cfg.MetainfoTimeout,
	}
	c.client.Store(client)

	return c, nil
}

// GetTorrentHandler adds a torrent from a URL or magnet link and waits for
// metadata. isNew reports whether this call attached the torrent to the
// client (false when the same infohash was already present, e.g. a torrent
// that is still seeding) — callers must not Drop handles they didn't add.
func (c *Client) GetTorrentHandler(ctx context.Context, url string, isMagnet bool) (*torrent.Torrent, bool, error) {
	if c == nil {
		return nil, false, ErrNilClient
	}

	client := c.GetClient()
	if client == nil {
		return nil, false, ErrNilClient
	}

	var (
		spec *torrent.TorrentSpec
		err  error
	)

	if isMagnet {
		spec, err = torrent.TorrentSpecFromMagnetUri(url)
		if err != nil {
			return nil, false, err
		}
	} else {
		// Bound the metainfo fetch — the origin server may never respond.
		fetchCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		mi, err := getMetainfo(fetchCtx, url)
		if err != nil {
			return nil, false, err
		}

		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			return nil, false, err
		}
	}

	t, isNew, err := client.AddTorrentSpec(spec)
	if err != nil {
		return nil, false, err
	}

	if err := c.waitForInfo(ctx, t); err != nil {
		// Don't leave a handle we just attached announcing/transferring
		// in the background after a failed add.
		if isNew {
			t.Drop()
		}

		return nil, false, err
	}

	return t, isNew, nil
}

// GetClient returns the underlying torrent client.
func (c *Client) GetClient() *torrent.Client {
	if c == nil {
		return nil
	}

	return c.client.Load()
}

// AddTorrent adds a torrent from metainfo.
func (c *Client) AddTorrent(mi *metainfo.MetaInfo) (*torrent.Torrent, error) {
	if c == nil {
		return nil, ErrNilClient
	}

	if mi == nil {
		return nil, ErrNilMetainfo
	}

	client := c.client.Load()
	if client == nil {
		return nil, ErrNilClient
	}

	return client.AddTorrent(mi)
}

// AddTorrentFromBytes adds a torrent from persisted metainfo bytes and waits
// for its info to be available (immediate for full metainfo). isNew reports
// whether this call attached the torrent — callers must not Drop handles
// they didn't add.
func (c *Client) AddTorrentFromBytes(ctx context.Context, data []byte) (*torrent.Torrent, bool, error) {
	if c == nil {
		return nil, false, ErrNilClient
	}

	client := c.GetClient()
	if client == nil {
		return nil, false, ErrNilClient
	}

	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse persisted metainfo: %w", err)
	}

	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return nil, false, err
	}

	t, isNew, err := client.AddTorrentSpec(spec)
	if err != nil {
		return nil, false, err
	}

	if err := c.waitForInfo(ctx, t); err != nil {
		if isNew {
			t.Drop()
		}

		return nil, false, err
	}

	return t, isNew, nil
}

// DropTorrent drops the torrent with the given hex info hash from the
// client, stopping any transfer (including background seeding).
func (c *Client) DropTorrent(infoHashHex string) {
	client := c.GetClient()
	if client == nil {
		return
	}

	var h metainfo.Hash
	if err := h.FromHexString(infoHashHex); err != nil {
		return
	}

	if t, ok := client.Torrent(h); ok {
		t.Drop()
	}
}

// AddMagnet adds a torrent from a magnet link.
func (c *Client) AddMagnet(magnetURI string) (*torrent.Torrent, error) {
	if c == nil {
		return nil, ErrNilClient
	}

	client := c.client.Load()
	if client == nil {
		return nil, ErrNilClient
	}

	return client.AddMagnet(magnetURI)
}

func (c *Client) Close() error {
	if c == nil {
		return ErrNilClient
	}

	old := c.client.Swap(nil)
	if old == nil {
		return ErrNilClient
	}

	old.Close()

	return nil
}

// waitForInfo blocks until the torrent's metadata is available, the context
// is cancelled, or the client's metadata timeout elapses.
func (c *Client) waitForInfo(ctx context.Context, t *torrent.Torrent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.timeout):
		return ErrMetadataTimeout
	case <-t.GotInfo():
		return nil
	}
}

// getMetainfo fetches and parses the metainfo from a given URL.
func getMetainfo(ctx context.Context, url string) (*metainfo.MetaInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", "err", err)
		}
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("%w: status %d", ErrMetainfoFetch, resp.StatusCode)
	}

	return metainfo.Load(resp.Body)
}
