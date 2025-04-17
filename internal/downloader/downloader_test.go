package downloader_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/protocol"
)

// fakeProtocol satisfies protocol.Protocol for testing download.
type fakeProtocol struct{}

func (f *fakeProtocol) CanHandle(url string) bool { return true }
func (f *fakeProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: 0, SupportsRanges: false}, nil
}
func (f *fakeProtocol) CreateConnection(urlStr string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return &fakeConn{url: urlStr}, nil
}
func (f *fakeProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}

// fakeConn implements connection.Connection; Read returns EOF immediately.
type fakeConn struct{ url string }

func (f *fakeConn) Connect(ctx context.Context) error               { return nil }
func (f *fakeConn) Read(ctx context.Context, p []byte) (int, error) { return 0, io.EOF }
func (f *fakeConn) Close() error                                    { return nil }
func (f *fakeConn) IsAlive() bool                                   { return true }
func (f *fakeConn) Reset(ctx context.Context) error                 { return nil }
func (f *fakeConn) GetURL() string                                  { return f.url }
func (f *fakeConn) GetHeaders() map[string]string                   { return nil }
func (f *fakeConn) SetHeader(key, value string)                     {}
func (f *fakeConn) SetTimeout(d time.Duration)                      {}

func TestNewDownload_EmptyURL(t *testing.T) {
	_, err := downloader.NewDownload(context.Background(), "", protocol.NewHandler(), &common.Config{}, nil)
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

func TestNewDownload_CreatesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	tmp := filepath.Join(t.TempDir(), "tmp")
	cfg := &common.Config{Directory: dir, TempDir: tmp, Connections: 1}

	proto := protocol.NewHandler()
	proto.RegisterProtocol(&fakeProtocol{})
	saveChan := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(context.Background(), "file://example", proto, cfg, saveChan)
	if err != nil {
		t.Fatalf("NewDownload failed: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("expected directory %s to exist", dir)
	}
	if _, err := os.Stat(tmp); os.IsNotExist(err) {
		t.Errorf("expected temp dir %s to exist", tmp)
	}
	if dl.URL != "file://example" {
		t.Errorf("expected URL to be set, got %s", dl.URL)
	}
}

func TestStart_CompletesImmediately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out2")
	tmp := filepath.Join(t.TempDir(), "tmp2")
	cfg := &common.Config{Directory: dir, TempDir: tmp, Connections: 1, MaxRetries: 1}

	proto := protocol.NewHandler()
	proto.RegisterProtocol(&fakeProtocol{})
	saveChan := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(context.Background(), "file://quick", proto, cfg, saveChan)
	if err != nil {
		t.Fatalf("NewDownload: %v", err)
	}

	pool := connection.NewPool(1, time.Minute)
	dl.Start(pool)

	if dl.GetStatus() != common.StatusCompleted {
		t.Errorf("expected Completed, got %v", dl.GetStatus())
	}
}

func TestStop_CancelsActiveDownload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out3")
	tmp := filepath.Join(t.TempDir(), "tmp3")
	cfg := &common.Config{Directory: dir, TempDir: tmp, Connections: 1, MaxRetries: 1}

	// blockingConn blocks until context cancellation
	blockConn := &blockingConn{}
	proto := protocol.NewHandler()
	proto.RegisterProtocol(&blockingProtocol{conn: blockConn})
	saveChan := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(context.Background(), "file://bar", proto, cfg, saveChan)
	if err != nil {
		t.Fatalf("NewDownload: %v", err)
	}

	pool := connection.NewPool(1, time.Minute)
	go dl.Start(pool)
	time.Sleep(50 * time.Millisecond)
	dl.Stop(common.StatusCancelled, false)

	if dl.GetStatus() != common.StatusCancelled {
		t.Errorf("expected status Cancelled, got %v", dl.GetStatus())
	}
}

func TestResume_Behavior(t *testing.T) {
	dl := &downloader.Download{}
	dl.SetStatus(common.StatusPaused)
	if !dl.Resume(context.Background()) {
		t.Error("Resume should allow Paused status")
	}
	dl.SetStatus(common.StatusFailed)
	if !dl.Resume(context.Background()) {
		t.Error("Resume should allow Failed status")
	}
	dl.SetStatus(common.StatusCompleted)
	if dl.Resume(context.Background()) {
		t.Error("Resume should disallow Completed status")
	}
	dl.SetStatus(common.StatusPending)
	if dl.Resume(context.Background()) {
		t.Error("Resume should disallow Pending status")
	}
}

// blockingConn simulates a hanging connection until cancellation.
type blockingConn struct{ url string }

func (b *blockingConn) Connect(ctx context.Context) error { return nil }
func (b *blockingConn) Read(ctx context.Context, p []byte) (int, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}
func (b *blockingConn) Close() error                    { return nil }
func (b *blockingConn) IsAlive() bool                   { return true }
func (b *blockingConn) Reset(ctx context.Context) error { return nil }
func (b *blockingConn) GetURL() string                  { return b.url }
func (b *blockingConn) GetHeaders() map[string]string   { return nil }
func (b *blockingConn) SetHeader(k, v string)           {}
func (b *blockingConn) SetTimeout(d time.Duration)      {}

// blockingProtocol returns a blockage connection tied to the URL.
type blockingProtocol struct{ conn *blockingConn }

func (b *blockingProtocol) CanHandle(url string) bool { return true }
func (b *blockingProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	b.conn.url = url
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: 1, SupportsRanges: false}, nil
}
func (b *blockingProtocol) CreateConnection(u string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return b.conn, nil
}
func (b *blockingProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}
