package chunk_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/NamanBalaji/tdm/internal/common"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/google/uuid"
)

// dummyConnection is a simple connection that returns predefined data then io.EOF.
type dummyConnection struct {
	data    []byte
	readPos int
}

func (d *dummyConnection) Connect() error { return nil }
func (d *dummyConnection) Read(p []byte) (int, error) {
	if d.readPos >= len(d.data) {
		return 0, io.EOF
	}
	n := copy(p, d.data[d.readPos:])
	d.readPos += n
	return n, nil
}
func (d *dummyConnection) Close() error                     { return nil }
func (d *dummyConnection) IsAlive() bool                    { return true }
func (d *dummyConnection) Reset() error                     { return nil }
func (d *dummyConnection) GetURL() string                   { return "dummy://connection" }
func (d *dummyConnection) GetHeaders() map[string]string    { return map[string]string{} }
func (d *dummyConnection) SetTimeout(timeout time.Duration) {}

// errorConnection always returns an error on Read.
type errorConnection struct {
	err error
}

func (e *errorConnection) Connect() error                   { return nil }
func (e *errorConnection) Read(p []byte) (int, error)       { return 0, e.err }
func (e *errorConnection) Close() error                     { return nil }
func (e *errorConnection) IsAlive() bool                    { return true }
func (e *errorConnection) Reset() error                     { return nil }
func (e *errorConnection) GetURL() string                   { return "dummy://error" }
func (e *errorConnection) GetHeaders() map[string]string    { return map[string]string{} }
func (e *errorConnection) SetTimeout(timeout time.Duration) {}

// slowConnection simulates a connection that delays returning data.
type slowConnection struct {
	data []byte
}

func (s *slowConnection) Connect() error { return nil }
func (s *slowConnection) Read(p []byte) (int, error) {
	// Sleep to simulate slow read.
	time.Sleep(500 * time.Millisecond)
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}
func (s *slowConnection) Close() error                     { return nil }
func (s *slowConnection) IsAlive() bool                    { return true }
func (s *slowConnection) Reset() error                     { return nil }
func (s *slowConnection) GetURL() string                   { return "dummy://slow" }
func (s *slowConnection) GetHeaders() map[string]string    { return map[string]string{} }
func (s *slowConnection) SetTimeout(timeout time.Duration) {}

func fakeProgress(i int64) {

}
func TestNewChunk_Size(t *testing.T) {
	downloadID := uuid.New()
	c := chunk.NewChunk(downloadID, 0, 99, fakeProgress) // Should be 100 bytes (0..99)
	if c.Size() != 100 {
		t.Errorf("expected size 100, got %d", c.Size())
	}
}

func TestReset(t *testing.T) {
	downloadID := uuid.New()
	c := chunk.NewChunk(downloadID, 0, 99, fakeProgress)
	atomic.StoreInt64(&c.Downloaded, 50)
	c.Status = common.StatusFailed
	c.Error = errors.New("dummy error")
	prevRetry := c.RetryCount
	c.Reset()
	if c.Status != common.StatusPending {
		t.Errorf("expected status %q, got %q", common.StatusPending, c.Status)
	}
	if c.Error != nil {
		t.Error("expected error to be nil after reset")
	}
	if c.RetryCount != prevRetry+1 {
		t.Errorf("expected retry count %d, got %d", prevRetry+1, c.RetryCount)
	}
}

func TestVerifyIntegrity(t *testing.T) {
	downloadID := uuid.New()
	c := chunk.NewChunk(downloadID, 0, 99, fakeProgress)
	atomic.StoreInt64(&c.Downloaded, 50)
	if c.VerifyIntegrity() {
		t.Error("expected integrity check to fail when downloaded != size")
	}
	atomic.StoreInt64(&c.Downloaded, 100)
	if !c.VerifyIntegrity() {
		t.Error("expected integrity check to pass when downloaded equals size")
	}
}

func TestDownload_Success(t *testing.T) {
	data := []byte("Hello, world!")
	downloadID := uuid.New()
	tmpFile, err := os.CreateTemp("", "chunk_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	c := chunk.NewChunk(downloadID, 0, int64(len(data))-1, fakeProgress)
	c.TempFilePath = tmpFile.Name()
	c.Connection = &dummyConnection{data: data}

	err = c.Download(context.Background())
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if c.Status != common.StatusCompleted {
		t.Errorf("expected status %q, got %q", common.StatusCompleted, c.Status)
	}

	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}
	if !bytes.Equal(content, data) {
		t.Errorf("expected file content %q, got %q", data, content)
	}
}

func TestDownload_CtxCancel(t *testing.T) {
	data := []byte("Some data that won't be fully read")
	downloadID := uuid.New()
	tmpFile, err := os.CreateTemp("", "chunk_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	c := chunk.NewChunk(downloadID, 0, int64(len(data))-1, fakeProgress)
	c.TempFilePath = tmpFile.Name()
	c.Connection = &slowConnection{data: data}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Download(ctx)
	if err == nil {
		t.Error("expected error due to context cancellation, got nil")
	}
	if c.Status != common.StatusPaused {
		t.Errorf("expected status %q due to cancellation, got %q", common.StatusPaused, c.Status)
	}
}

func TestDownload_Error(t *testing.T) {
	downloadID := uuid.New()
	tmpFile, err := os.CreateTemp("", "chunk_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	c := chunk.NewChunk(downloadID, 0, 99, fakeProgress)
	c.TempFilePath = tmpFile.Name()
	expectedErr := errors.New("dummy read error")
	c.Connection = &errorConnection{err: expectedErr}

	err = c.Download(context.Background())
	if err == nil {
		t.Error("expected error from Download, got nil")
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("expected error %q, got %q", expectedErr, err)
	}
	if c.Status != common.StatusFailed {
		t.Errorf("expected status %q, got %q", common.StatusFailed, c.Status)
	}
}
