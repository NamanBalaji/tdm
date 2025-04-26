package downloader_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/protocol"
	httpProto "github.com/NamanBalaji/tdm/internal/protocol/http"
)

type fakeProtocol struct{}

func (f *fakeProtocol) CanHandle(url string) bool { return true }
func (f *fakeProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: 0, SupportsRanges: false}, nil
}
func (f *fakeProtocol) CreateConnection(u string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return &fakeConn{url: u}, nil
}
func (f *fakeProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}

type fakeConn struct{ url string }

func (f *fakeConn) Connect(ctx context.Context) error               { return nil }
func (f *fakeConn) Read(ctx context.Context, p []byte) (int, error) { return 0, io.EOF }
func (f *fakeConn) Close() error                                    { return nil }
func (f *fakeConn) IsAlive() bool                                   { return true }
func (f *fakeConn) Reset(ctx context.Context) error                 { return nil }
func (f *fakeConn) GetURL() string                                  { return f.url }
func (f *fakeConn) GetHeaders() map[string]string                   { return nil }
func (f *fakeConn) SetHeader(k, v string)                           {}
func (f *fakeConn) SetTimeout(d time.Duration)                      {}

type flappyProtocol struct{}

func (f *flappyProtocol) CanHandle(url string) bool { return true }
func (f *flappyProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: 1, SupportsRanges: false}, nil
}
func (f *flappyProtocol) CreateConnection(u string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return &flappyConn{url: u}, nil
}
func (f *flappyProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}

type flappyConn struct {
	url   string
	tries int
}

func (f *flappyConn) Connect(ctx context.Context) error { return nil }
func (f *flappyConn) Read(ctx context.Context, p []byte) (int, error) {
	f.tries++
	if f.tries == 1 {
		return 0, httpProto.ErrNetworkProblem
	}
	return 0, io.EOF
}
func (f *flappyConn) Close() error                    { return nil }
func (f *flappyConn) IsAlive() bool                   { return true }
func (f *flappyConn) Reset(ctx context.Context) error { return nil }
func (f *flappyConn) GetURL() string                  { return f.url }
func (f *flappyConn) GetHeaders() map[string]string   { return nil }
func (f *flappyConn) SetHeader(k, v string)           {}
func (f *flappyConn) SetTimeout(d time.Duration)      {}

type rangedProtocol struct{}

func (r *rangedProtocol) CanHandle(url string) bool { return true }
func (r *rangedProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: chunk.MinChunkSize * 4, SupportsRanges: true}, nil
}
func (r *rangedProtocol) CreateConnection(u string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return &fakeConn{url: u}, nil
}
func (r *rangedProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}

func TestManualStatsFields(t *testing.T) {
	d := &downloader.Download{TotalSize: 100}
	d.SetStatus(common.StatusActive)
	d.SetTotalChunks(2)
	// No bytes added
	c := chunk.NewChunk(d.ID, 0, 49, nil)
	c.SetStatus(common.StatusCompleted)
	d.Chunks = []*chunk.Chunk{c}

	stats := d.GetStats()
	if stats.Progress != 0 {
		t.Errorf("expected 0%% progress, got %.0f", stats.Progress)
	}
	if stats.CompletedChunks != 1 {
		t.Errorf("expected 1 completed chunk, got %d", stats.CompletedChunks)
	}
	if stats.TotalChunks != 2 {
		t.Errorf("expected total chunks 2, got %d", stats.TotalChunks)
	}
}

func TestEmptyURLError(t *testing.T) {
	_, err := downloader.NewDownload(context.Background(), "", protocol.NewHandler(), &common.Config{}, make(chan *downloader.Download, 1))
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestNewDownloadSuccess(t *testing.T) {
	tmp := t.TempDir()
	cfg := &common.Config{Directory: tmp, TempDir: tmp, Connections: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&fakeProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(context.Background(), "file://x", h, cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	if dl.GetTotalChunks() != 1 {
		t.Errorf("expected 1 chunk, got %d", dl.GetTotalChunks())
	}
}

func TestStartAndComplete(t *testing.T) {
	tmp := t.TempDir()
	cfg := &common.Config{Directory: tmp, TempDir: tmp, Connections: 1, MaxRetries: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&fakeProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, _ := downloader.NewDownload(context.Background(), "file://stats", h, cfg, ch)
	pool := connection.NewPool(1, time.Second)
	dl.Start(context.Background(), pool)
	if dl.GetStatus() != common.StatusCompleted {
		t.Errorf("expected Completed, got %v", dl.GetStatus())
	}
}

func TestPauseCancelResume(t *testing.T) {
	d := &downloader.Download{}
	d.SetStatus(common.StatusPending)
	d.Stop(common.StatusPaused, false)
	if d.GetStatus() != common.StatusPending {
		t.Errorf("expected Pending after Pause on pending, got %v", d.GetStatus())
	}
	d.SetStatus(common.StatusPending)
	d.Stop(common.StatusCancelled, false)
	if d.GetStatus() != common.StatusPending {
		t.Errorf("expected Pending after Cancel on pending, got %v", d.GetStatus())
	}
	d.SetStatus(common.StatusPaused)
	if !d.Resume(context.Background()) {
		t.Error("should resume Paused")
	}
	d.SetStatus(common.StatusFailed)
	if !d.Resume(context.Background()) {
		t.Error("should resume Failed")
	}
}

func TestRetryableError(t *testing.T) {
	tmp := t.TempDir()
	cfg := &common.Config{Directory: tmp, TempDir: tmp, Connections: 1, MaxRetries: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&flappyProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, _ := downloader.NewDownload(context.Background(), "file://retry", h, cfg, ch)
	pool := connection.NewPool(1, time.Second)
	dl.Start(context.Background(), pool)
	if dl.GetStatus() != common.StatusCompleted {
		t.Errorf("retry failed, got %v", dl.GetStatus())
	}
}

func TestMultiChunkCreation(t *testing.T) {
	tmp := t.TempDir()
	cfg := &common.Config{Directory: tmp, TempDir: tmp, Connections: 4}
	h := protocol.NewHandler()
	h.RegisterProtocol(&rangedProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, _ := downloader.NewDownload(context.Background(), "http://example.com/file", h, cfg, ch)
	if dl.GetTotalChunks() <= 1 {
		t.Errorf("expected multiple chunks, got %d", dl.GetTotalChunks())
	}
}
