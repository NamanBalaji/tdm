package downloader_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

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

type flappyProtocol struct{}

func (f *flappyProtocol) CanHandle(url string) bool { return true }
func (f *flappyProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: 1, SupportsRanges: false}, nil
}
func (f *flappyProtocol) CreateConnection(u string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return &flappyConn{url: u}, nil
}
func (f *flappyProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}

type rangedConn struct{ url string }

func (r *rangedConn) Connect(ctx context.Context) error               { return nil }
func (r *rangedConn) Read(ctx context.Context, p []byte) (int, error) { return 0, io.EOF }
func (r *rangedConn) Close() error                                    { return nil }
func (r *rangedConn) IsAlive() bool                                   { return true }
func (r *rangedConn) Reset(ctx context.Context) error                 { return nil }
func (r *rangedConn) GetURL() string                                  { return r.url }
func (r *rangedConn) GetHeaders() map[string]string                   { return nil }
func (r *rangedConn) SetHeader(k, v string)                           {}
func (r *rangedConn) SetTimeout(d time.Duration)                      {}

type rangedProtocol struct{}

func (r *rangedProtocol) CanHandle(url string) bool { return true }
func (r *rangedProtocol) Initialize(ctx context.Context, url string, cfg *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{URL: url, Filename: filepath.Base(url), TotalSize: 1024 * 1024, SupportsRanges: true}, nil
}
func (r *rangedProtocol) CreateConnection(u string, c *chunk.Chunk, cfg *common.Config) (connection.Connection, error) {
	return &rangedConn{url: u}, nil
}
func (r *rangedProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {}

func TestNewDownloadErrorEmptyURL(t *testing.T) {
	_, err := downloader.NewDownload(t.Context(), "", protocol.NewHandler(), &common.Config{}, make(chan *downloader.Download, 1))
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestNewDownloadSuccess(t *testing.T) {
	d := filepath.Join(t.TempDir(), "out")
	tmp := filepath.Join(t.TempDir(), "tmp")
	cfg := &common.Config{Directory: d, TempDir: tmp, Connections: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&fakeProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(t.Context(), "file://x", h, cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	if dl.GetTotalChunks() != 1 {
		t.Errorf("expected 1 chunk, got %d", dl.GetTotalChunks())
	}
}

func TestStartAndStats(t *testing.T) {
	d := filepath.Join(t.TempDir(), "sd")
	tmp := filepath.Join(t.TempDir(), "st")
	cfg := &common.Config{Directory: d, TempDir: tmp, Connections: 1, MaxRetries: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&fakeProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(t.Context(), "file://stats", h, cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	pool := connection.NewPool(1, time.Second)
	dl.Start(t.Context(), pool)
	if dl.GetStatus() != common.StatusCompleted {
		t.Errorf("expected Completed, got %v", dl.GetStatus())
	}
	stats := dl.GetStats()
	if stats.TotalChunks != 1 {
		t.Errorf("expected TotalChunks=1, got %d", stats.TotalChunks)
	}
}

func TestStopCancels(t *testing.T) {
	d := filepath.Join(t.TempDir(), "ld")
	tmp := filepath.Join(t.TempDir(), "lt")
	cfg := &common.Config{Directory: d, TempDir: tmp, Connections: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&blockingProtocol{conn: &blockingConn{}})
	ch := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(t.Context(), "file://block", h, cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	pool := connection.NewPool(1, time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	go dl.Start(ctx, pool)
	time.Sleep(10 * time.Millisecond)
	cancel()
	dl.Stop(common.StatusCancelled, false)
	if dl.GetStatus() != common.StatusCancelled {
		t.Errorf("expected Cancelled, got %v", dl.GetStatus())
	}
}

func TestRemove(t *testing.T) {
	d := filepath.Join(t.TempDir(), "rm")
	os.MkdirAll(d, 0755)
	file := filepath.Join(d, "f.txt")
	os.WriteFile(file, []byte("x"), 0644)
	dl := &downloader.Download{Config: &common.Config{Directory: d}, Filename: "f.txt"}
	dl.Remove()
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file not deleted: %v", err)
	}
}

func TestResumeBehavior(t *testing.T) {
	dl := &downloader.Download{}
	dl.SetStatus(common.StatusPending)
	if dl.Resume(t.Context()) {
		t.Error("should not resume Pending")
	}
	dl.SetStatus(common.StatusPaused)
	if !dl.Resume(t.Context()) {
		t.Error("should resume Paused")
	}
	dl.SetStatus(common.StatusFailed)
	if !dl.Resume(t.Context()) {
		t.Error("should resume Failed")
	}
}

func TestRetryLogic(t *testing.T) {
	d := filepath.Join(t.TempDir(), "rl")
	tmp := filepath.Join(t.TempDir(), "rt")
	cfg := &common.Config{Directory: d, TempDir: tmp, Connections: 1, MaxRetries: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&flappyProtocol{})
	ch := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(t.Context(), "file://retry", h, cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	pool := connection.NewPool(1, time.Second)
	dl.Start(t.Context(), pool)
	if dl.GetStatus() != common.StatusCompleted {
		t.Errorf("retry failed, got %v", dl.GetStatus())
	}
}

func TestManualStatsFields(t *testing.T) {
	d := &downloader.Download{
		TotalSize:  100,
		Downloaded: 50,
		StartTime:  time.Now().Add(-5 * time.Second),
	}
	d.SetStatus(common.StatusActive)
	d.SetTotalChunks(2)
	c := chunk.NewChunk(d.ID, 0, 49, func(int64) {})
	c.SetStatus(common.StatusCompleted)
	d.Chunks = []*chunk.Chunk{c}

	stats := d.GetStats()
	if stats.Progress != 50 {
		t.Errorf("expected 50%% progress, got %.0f", stats.Progress)
	}
	if stats.CompletedChunks != 1 {
		t.Errorf("expected 1 completed chunk, got %d", stats.CompletedChunks)
	}
	if stats.TotalChunks != 2 {
		t.Errorf("expected total chunks 2, got %d", stats.TotalChunks)
	}
}

func TestMultiChunkCreation(t *testing.T) {
	d := filepath.Join(t.TempDir(), "mchunks")
	tmp := filepath.Join(t.TempDir(), "tchunks")
	cfg := &common.Config{Directory: d, TempDir: tmp, Connections: 4}
	r := &rangedProtocol{}
	h := protocol.NewHandler()
	h.RegisterProtocol(r)
	ch := make(chan *downloader.Download, 1)
	dl, err := downloader.NewDownload(t.Context(), "http://example.com/file", h, cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	if dl.GetTotalChunks() <= 1 {
		t.Errorf("expected multiple chunks, got %d", dl.GetTotalChunks())
	}
}

func TestPrepareForSerializationLength(t *testing.T) {
	u := uuid.New()
	d := &downloader.Download{
		Chunks: []*chunk.Chunk{
			chunk.NewChunk(u, 0, 9, func(int64) {}),
			chunk.NewChunk(u, 10, 19, func(int64) {}),
		},
	}
	d.SetTotalChunks(2)
	d.Downloaded = 5

	d.PrepareForSerialization()
	if len(d.ChunkInfos) != 2 {
		t.Errorf("expected 2 chunk infos, got %d", len(d.ChunkInfos))
	}
}

func TestPrepareRestoreSerialization(t *testing.T) {
	// Round-trip through PrepareForSerialization and RestoreFromSerialization
	dir := filepath.Join(t.TempDir(), "prs")
	tmp := filepath.Join(t.TempDir(), "prt")
	cfg := &common.Config{Directory: dir, TempDir: tmp, Connections: 1}
	h := protocol.NewHandler()
	h.RegisterProtocol(&fakeProtocol{})
	save := make(chan *downloader.Download, 1)
	dl1, err := downloader.NewDownload(t.Context(), "file://prs", h, cfg, save)
	if err != nil {
		t.Fatal(err)
	}
	dl1.SetStatus(common.StatusPaused)
	dl1.Downloaded = 42
	dl1.PrepareForSerialization()

	// Simulate persistence/load
	dl2 := &downloader.Download{
		ID:         dl1.ID,
		URL:        dl1.URL,
		Filename:   dl1.Filename,
		Config:     cfg,
		ChunkInfos: dl1.ChunkInfos,
		Status:     dl1.GetStatus(),
		Downloaded: dl1.GetDownloaded(),
	}
	err = dl2.RestoreFromSerialization(t.Context(), h, save)
	if err != nil {
		t.Fatal(err)
	}
	if dl2.GetStatus() != common.StatusPaused {
		t.Errorf("expected status Paused, got %v", dl2.GetStatus())
	}
	if dl2.GetDownloaded() != 42 {
		t.Errorf("expected downloaded=42, got %d", dl2.GetDownloaded())
	}
}

func TestErrorMessageStats(t *testing.T) {
	// Use ErrorMessage to simulate persisted error
	d := &downloader.Download{ErrorMessage: "persisted error"}
	stats := d.GetStats()
	if stats.Error != "persisted error" {
		t.Errorf("expected stats.Error='persisted error', got %q", stats.Error)
	}
}

func TestExternalFlag(t *testing.T) {
	d := &downloader.Download{}
	if d.GetIsExternal() {
		t.Error("expected default IsExternal=false")
	}
	d.SetIsExternal(true)
	if !d.GetIsExternal() {
		t.Error("expected IsExternal=true")
	}
	d.SetIsExternal(false)
	if d.GetIsExternal() {
		t.Error("expected IsExternal=false")
	}
}
