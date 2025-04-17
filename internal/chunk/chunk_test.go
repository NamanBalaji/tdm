package chunk_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
)

type fakeConn struct {
	data  []byte
	pos   int
	errAt int
	err   error
}

func (f *fakeConn) Connect(ctx context.Context) error { return nil }
func (f *fakeConn) Read(ctx context.Context, p []byte) (int, error) {
	if f.err != nil && f.pos >= f.errAt {
		return 0, f.err
	}
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}
func (f *fakeConn) Close() error                     { return nil }
func (f *fakeConn) IsAlive() bool                    { return true }
func (f *fakeConn) Reset(ctx context.Context) error  { return nil }
func (f *fakeConn) GetURL() string                   { return "" }
func (f *fakeConn) GetHeaders() map[string]string    { return nil }
func (f *fakeConn) SetHeader(key, value string)      {}
func (f *fakeConn) SetTimeout(timeout time.Duration) {}

func TestNewChunk(t *testing.T) {
	dlID := uuid.New()
	called := false
	fn := func(n int64) { called = true }
	c := chunk.NewChunk(dlID, 5, 15, fn)
	if c.DownloadID != dlID {
		t.Errorf("DownloadID mismatch: expected %v, got %v", dlID, c.DownloadID)
	}
	if c.GetStartByte() != 5 || c.GetEndByte() != 15 {
		t.Errorf("Byte range mismatch: expected 5-15, got %d-%d", c.GetStartByte(), c.GetEndByte())
	}
	if c.GetStatus() != common.StatusPending {
		t.Errorf("Expected status Pending, got %v", c.GetStatus())
	}
	if called {
		t.Errorf("progressFn invoked unexpectedly on NewChunk")
	}
}

func TestGetSetStatus(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	c.SetStatus(common.StatusActive)
	if c.GetStatus() != common.StatusActive {
		t.Errorf("Expected status Active, got %v", c.GetStatus())
	}
}

func TestGetSetStartEndByte(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	c.SetStartByte(3)
	c.SetEndByte(8)
	if c.GetStartByte() != 3 || c.GetEndByte() != 8 {
		t.Errorf("Expected byte range 3-8, got %d-%d", c.GetStartByte(), c.GetEndByte())
	}
}

func TestGetSetDownloadedAndAdd(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	c.SetDownloaded(4)
	if c.GetDownloaded() != 4 {
		t.Errorf("Expected Downloaded 4, got %d", c.GetDownloaded())
	}
	newVal := c.AddDownloaded(3)
	if newVal != 7 || c.GetDownloaded() != 7 {
		t.Errorf("AddDownloaded incorrect: got %d, want 7", newVal)
	}
}

func TestSizeAndCurrentByteRange(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 10, 19, nil)
	expectedSize := int64(10)
	if c.Size() != expectedSize {
		t.Errorf("Size mismatch: expected %d, got %d", expectedSize, c.Size())
	}
	c.SetDownloaded(4)
	start, end := c.GetCurrentByteRange()
	if start != 14 || end != 19 {
		t.Errorf("Current byte range mismatch: expected 14-19, got %d-%d", start, end)
	}
}

func TestCanDownload(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	if !c.CanDownload() {
		t.Errorf("Expected CanDownload for Pending status")
	}
	c.SetStatus(common.StatusActive)
	if c.CanDownload() {
		t.Errorf("Expected CanDownload to be false when Active")
	}
	c.SetStatus(common.StatusPaused)
	if !c.CanDownload() {
		t.Errorf("Expected CanDownload true when Paused")
	}
}

func TestRetryCount(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	c.SetRetryCount(5)
	if c.GetRetryCount() != 5 {
		t.Errorf("Expected RetryCount 5, got %d", c.GetRetryCount())
	}
}

func TestSetError(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	errTest := errors.New("test error")
	c.SetError(errTest)
	if !errors.Is(c.Error, errTest) {
		t.Errorf("Expected Error to be %v, got %v", errTest, c.Error)
	}
}

func TestSetConnection(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	f := &fakeConn{}
	c.SetConnection(f)
	if c.Connection != f {
		t.Errorf("Expected Connection to be set to fakeConn instance")
	}
}

func TestVerifyIntegrity(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 4, nil)
	// size = 5
	c.SetDownloaded(5)
	if !c.VerifyIntegrity() {
		t.Errorf("Expected integrity to pass when downloaded == size")
	}
	c.SetDownloaded(3)
	if c.VerifyIntegrity() {
		t.Errorf("Expected integrity to fail when downloaded != size")
	}
}

func TestReset(t *testing.T) {
	c := chunk.NewChunk(uuid.New(), 0, 9, nil)
	errTest := errors.New("fail")
	f := &fakeConn{}
	c.SetConnection(f)
	c.SetError(errTest)
	c.RetryCount = 2
	c.SetStatus(common.StatusFailed)
	before := time.Now()
	c.Reset()
	if c.GetStatus() != common.StatusPending {
		t.Errorf("Expected status Pending after reset, got %v", c.GetStatus())
	}
	if c.Error != nil {
		t.Errorf("Expected Error to be nil after reset, got %v", c.Error)
	}
	if c.Connection != nil {
		t.Errorf("Expected Connection to be nil after reset")
	}
	if c.GetRetryCount() != 3 {
		t.Errorf("Expected RetryCount to increment by 1, got %d", c.GetRetryCount())
	}
	if c.LastActive.Before(before) {
		t.Errorf("Expected LastActive updated after reset")
	}
}

func TestDownloadSuccess(t *testing.T) {
	// prepare data and fake connection
	data := []byte("HelloWorld")
	f := &fakeConn{data: data}
	calls := []int64{}
	fn := func(n int64) { calls = append(calls, n) }

	c := chunk.NewChunk(uuid.New(), 0, int64(len(data)-1), fn)
	d := t.TempDir()
	path := filepath.Join(d, "chunk.tmp")
	c.TempFilePath = path
	c.SetConnection(f)

	err := c.Download(t.Context())
	if err != nil {
		t.Fatalf("Download returned unexpected error: %v", err)
	}
	if c.GetStatus() != common.StatusCompleted {
		t.Errorf("Expected status Completed, got %v", c.GetStatus())
	}
	if c.GetDownloaded() != int64(len(data)) {
		t.Errorf("Downloaded bytes mismatch: expected %d, got %d", len(data), c.GetDownloaded())
	}
	if len(calls) != 1 || calls[0] != int64(len(data)) {
		t.Errorf("Progress function calls mismatch: %v", calls)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed reading temp file: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("File content mismatch: expected %s, got %s", string(data), string(got))
	}
}

func TestDownloadError(t *testing.T) {
	errt := errors.New("read error")
	f := &fakeConn{data: []byte(""), errAt: 0, err: errt}
	c := chunk.NewChunk(uuid.New(), 0, 4, nil)
	d := t.TempDir()
	path := filepath.Join(d, "chunk_error.tmp")
	c.TempFilePath = path
	c.SetConnection(f)

	err := c.Download(t.Context())
	if err == nil {
		t.Fatal("Expected error from Download, got nil")
	}
	if c.GetStatus() != common.StatusFailed {
		t.Errorf("Expected status Failed, got %v", c.GetStatus())
	}
	if !errors.Is(c.Error, errt) {
		t.Errorf("Expected chunk.Error to be %v, got %v", errt, c.Error)
	}
}
