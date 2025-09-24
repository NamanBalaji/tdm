package torrent

import (
	"context"
	"errors"
	"sync"
	"time"

	analog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var (
	ErrTorrentClosed   = errors.New("torrent handler is closed")
	ErrNilClient       = errors.New("client is nil")
	ErrMetadataTimeout = errors.New("timeout waiting for metadata")
)

// Client wraps the anacrolix torrent client with thread-safe operations.
type Client struct {
	mu     sync.RWMutex
	client *torrent.Client
	config *torrent.ClientConfig
}

// NewClient creates a new torrent client with optimized configuration.
func NewClient(dataDir string) (*Client, error) {
	analog.Default.SetHandlers(analog.DiscardHandler)

	config := torrent.NewDefaultClientConfig()

	config.DataDir = dataDir
	config.Seed = true

	// CRITICAL FIX: Disable UTP to prevent memory leaks
	// See: https://github.com/anacrolix/torrent/issues/392
	config.DisableUTP = true

	config.EstablishedConnsPerTorrent = 50
	config.HalfOpenConnsPerTorrent = 25
	config.TotalHalfOpenConns = 100

	config.NoDHT = false      // Enable DHT
	config.DisablePEX = false // Enable Peer Exchange
	config.DisableTrackers = false
	config.DisableIPv6 = false

	config.DefaultStorage = storage.NewFile(config.DataDir)

	client, err := torrent.NewClient(config)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: client,
		config: config,
	}, nil
}

// GetMetainfoFromMagnet fetches torrent metadata from a magnet link.
func (c *Client) GetMetainfoFromMagnet(ctx context.Context, magnetURI string) (*metainfo.MetaInfo, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, ErrNilClient
	}

	t, err := client.AddMagnet(magnetURI)
	if err != nil {
		return nil, err
	}
	defer t.Drop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case <-t.GotInfo():
		mi := t.Metainfo()
		return &mi, nil

	case <-t.Closed():
		return nil, ErrTorrentClosed

	case <-time.After(60 * time.Second):
		return nil, ErrMetadataTimeout
	}
}

// GetClient returns the underlying torrent client.
func (c *Client) GetClient() *torrent.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client
}

// AddTorrent adds a torrent from metainfo.
func (c *Client) AddTorrent(mi *metainfo.MetaInfo) (*torrent.Torrent, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, ErrNilClient
	}

	return client.AddTorrent(mi)
}

// AddMagnet adds a torrent from a magnet link.
func (c *Client) AddMagnet(magnetURI string) (*torrent.Torrent, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, ErrNilClient
	}

	return client.AddMagnet(magnetURI)
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return ErrNilClient
	}

	c.client.Close()
	c.client = nil

	return nil
}
