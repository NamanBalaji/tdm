package torrent

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

func createMetaInfo(t *testing.T) *metainfo.MetaInfo {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "testfile.txt")
	err := os.WriteFile(filePath, []byte("test content"), 0666)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	info := metainfo.Info{
		PieceLength: 32768,
	}
	err = info.BuildFromFilePath(filePath)
	if err != nil {
		t.Fatalf("failed to build from file path: %v", err)
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal info: %v", err)
	}

	mi := &metainfo.MetaInfo{
		InfoBytes: infoBytes,
	}

	return mi
}

func createMetaInfoBytes(t *testing.T) []byte {
	mi := createMetaInfo(t)
	var buf bytes.Buffer
	err := mi.Write(&buf)
	if err != nil {
		t.Fatalf("failed to write metainfo: %v", err)
	}
	return buf.Bytes()
}

func newTestClient(t *testing.T) *Client {
	config := torrent.NewDefaultClientConfig()
	config.DataDir = t.TempDir()
	config.Seed = true
	config.DisableUTP = true
	config.EstablishedConnsPerTorrent = 50
	config.HalfOpenConnsPerTorrent = 25
	config.TotalHalfOpenConns = 100
	config.NoDHT = true // Disable for tests to avoid network
	config.DisablePEX = true
	config.DisableTrackers = true
	config.DisableIPv6 = true
	config.DefaultStorage = storage.NewFile(config.DataDir)
	config.ListenPort = 0 // Pick random port to avoid conflicts
	client, err := torrent.NewClient(config)
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}
	return &Client{
		client: client,
		config: config,
	}
}

func TestNewClient(t *testing.T) {
	dataDir := t.TempDir()
	c, err := NewClient(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.client == nil {
		t.Error("expected non-nil client")
	}
	if !c.config.DisableUTP {
		t.Error("expected DisableUTP to be true")
	}
	if c.config.EstablishedConnsPerTorrent != 50 {
		t.Error("expected EstablishedConnsPerTorrent to be 50")
	}
	if c.config.HalfOpenConnsPerTorrent != 25 {
		t.Error("expected HalfOpenConnsPerTorrent to be 25")
	}
	if c.config.TotalHalfOpenConns != 100 {
		t.Error("expected TotalHalfOpenConns to be 100")
	}
	if c.config.NoDHT {
		t.Error("expected NoDHT to be false")
	}
	if c.config.DisablePEX {
		t.Error("expected DisablePEX to be false")
	}
	if c.config.DisableTrackers {
		t.Error("expected DisableTrackers to be false")
	}
	if c.config.DisableIPv6 {
		t.Error("expected DisableIPv6 to be false")
	}
	if err := c.Close(); err != nil {
		t.Errorf("failed to close client: %v", err)
	}
}

func TestClient_Close(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *Client
		wantErr error
	}{
		{
			name: "nil client",
			setup: func() *Client {
				return nil
			},
			wantErr: ErrNilClient,
		},
		{
			name: "nil internal client",
			setup: func() *Client {
				return &Client{}
			},
			wantErr: ErrNilClient,
		},
		{
			name: "valid client",
			setup: func() *Client {
				return newTestClient(t)
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.setup()
			err := c.Close()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
			if c != nil && c.client != nil {
				t.Error("expected internal client to be nil after close")
			}
		})
	}
}

func TestClient_GetClient(t *testing.T) {
	c := newTestClient(t)
	if c.GetClient() == nil {
		t.Error("expected non-nil client")
	}
	if err := c.Close(); err != nil {
		t.Errorf("failed to close: %v", err)
	}
	if c.GetClient() != nil {
		t.Error("expected nil client after close")
	}
	if (&Client{}).GetClient() != nil {
		t.Error("expected nil for uninitialized client")
	}
}

func TestClient_AddTorrent(t *testing.T) {
	c := newTestClient(t)
	mi := createMetaInfo(t)
	tests := []struct {
		name    string
		c       *Client
		mi      *metainfo.MetaInfo
		wantErr error
	}{
		{"nil client", nil, mi, ErrNilClient},
		{"nil metainfo", c, nil, ErrNilMetainfo},
		{"nil internal client", &Client{}, mi, ErrNilClient},
		{"valid", c, mi, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := tt.c.AddTorrent(tt.mi)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
			if err == nil {
				if tr == nil {
					t.Error("expected non-nil torrent")
				}
				if tr.InfoHash() != mi.HashInfoBytes() {
					t.Error("infohash mismatch")
				}
			}
		})
	}
}

func TestClient_AddMagnet(t *testing.T) {
	c := newTestClient(t)
	mi := createMetaInfo(t)
	validMagnet := "magnet:?xt=urn:btih:" + mi.HashInfoBytes().HexString()
	tests := []struct {
		name    string
		c       *Client
		magnet  string
		wantErr bool
	}{
		{"nil client", nil, validMagnet, true},
		{"nil internal client", &Client{}, validMagnet, true},
		{"invalid magnet", c, "invalid", true},
		{"valid magnet", c, validMagnet, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := tt.c.AddMagnet(tt.magnet)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tr == nil {
				t.Error("expected non-nil torrent")
			}
		})
	}
}

func Test_getMetainfo(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() string // returns URL
		wantErr bool
	}{
		{
			name: "valid metainfo",
			setup: func() string {
				b := createMetaInfoBytes(t)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write(b)
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantErr: false,
		},
		{
			name: "invalid metainfo",
			setup: func() string {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("invalid"))
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantErr: true,
		},
		{
			name: "connection error",
			setup: func() string {
				return "http://127.0.0.1:99999/invalid"
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.setup()
			mi, err := getMetainfo(url)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && mi == nil {
				t.Error("expected non-nil metainfo")
			}
		})
	}
}

func TestGetTorrentHandler_NonMagnet_Success(t *testing.T) {
	c := newTestClient(t)
	mi := createMetaInfo(t)
	b := createMetaInfoBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(b)
	}))
	defer srv.Close()
	tr, err := c.GetTorrentHandler(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Error("expected non-nil torrent")
	}
	if tr.InfoHash() != mi.HashInfoBytes() {
		t.Error("infohash mismatch")
	}
}

func TestGetTorrentHandler_NonMagnet_Error(t *testing.T) {
	c := newTestClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid"))
	}))
	defer srv.Close()
	_, err := c.GetTorrentHandler(context.Background(), srv.URL, false)
	if err == nil {
		t.Error("expected error")
	}
}

func TestGetTorrentHandler_Magnet_Success(t *testing.T) {
	c := newTestClient(t)
	mi := createMetaInfo(t)
	_, err := c.AddTorrent(mi)
	if err != nil {
		t.Fatalf("failed to pre-add torrent: %v", err)
	}
	magnet := "magnet:?xt=urn:btih:" + mi.HashInfoBytes().HexString()
	tr, err := c.GetTorrentHandler(context.Background(), magnet, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Error("expected non-nil torrent")
	}
	select {
	case <-tr.GotInfo():
		// expected
	default:
		t.Error("expected GotInfo to be signaled")
	}
}

func TestGetTorrentHandler_Magnet_Timeout(t *testing.T) {
	// To make the test fast, we'll use a short context timeout and note that in practice it covers the wait,
	// but to hit ErrMetadataTimeout specifically, the full wait is needed. For practicality, we'll check for timeout error.
	c := newTestClient(t)
	magnet := "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.GetTorrentHandler(ctx, magnet, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want %v", err, context.DeadlineExceeded)
	}
}
