package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/status"
)

type mockDownloader struct {
	reader io.Reader
	err    error
	block  chan struct{}
}

func (m *mockDownloader) Read(ctx context.Context, p []byte) (n int, err error) {
	if m.err != nil {
		return 0, m.err
	}

	if m.block != nil {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-m.block:
			return 0, io.EOF
		}
	}

	return m.reader.Read(p)
}

func setupChunkTest(t *testing.T) (string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "chunk_test_*")
	require.NoError(t, err)
	cleanup := func() {
		assert.NoError(t, os.RemoveAll(tempDir))
	}
	return tempDir, cleanup
}

func TestNewChunk(t *testing.T) {
	tempDir, cleanup := setupChunkTest(t)
	defer cleanup()

	c := newChunk(0, 99, tempDir)

	assert.NotEqual(t, [16]byte{}, c.ID, "ID should be initialized")
	assert.Equal(t, int64(0), c.StartByte)
	assert.Equal(t, int64(99), c.EndByte)
	assert.Equal(t, status.Pending, c.getStatus())
	assert.Contains(t, c.TempFilePath, tempDir, "TempFilePath should be inside the provided path")
	assert.Equal(t, int64(100), c.getTotalSize())
}

func TestChunk_StateManagement(t *testing.T) {
	tempDir, cleanup := setupChunkTest(t)
	defer cleanup()

	c := newChunk(0, 999, tempDir)

	assert.Equal(t, status.Pending, c.getStatus())
	c.setStatus(status.Active)
	assert.Equal(t, status.Active, c.getStatus())

	assert.Equal(t, int64(0), c.getDownloaded())
	c.updateDownloaded(50)
	assert.Equal(t, int64(50), c.getDownloaded())
	c.updateDownloaded(100)
	assert.Equal(t, int64(150), c.getDownloaded())

	assert.Equal(t, int32(0), c.getRetryCount())
	c.setRetryCount(2)
	assert.Equal(t, int32(2), c.getRetryCount())

	assert.Equal(t, int64(0), c.getStartByte())
	assert.Equal(t, int64(999), c.getEndByte())
	assert.Equal(t, filepath.Join(tempDir, c.ID.String()), c.getTempFilePath())
}

func TestChunk_Download(t *testing.T) {
	tempDir, cleanup := setupChunkTest(t)
	defer cleanup()

	t.Run("successful download", func(t *testing.T) {
		chunk := newChunk(0, 99, tempDir)
		testData := bytes.Repeat([]byte("a"), 100)
		downloader := &mockDownloader{reader: bytes.NewReader(testData)}

		err := chunk.Download(context.Background(), downloader, false)
		require.NoError(t, err)

		assert.Equal(t, status.Completed, chunk.getStatus())
		assert.Equal(t, int64(100), chunk.getDownloaded())

		fileContent, err := os.ReadFile(chunk.getTempFilePath())
		require.NoError(t, err)
		assert.Equal(t, testData, fileContent)
	})

	t.Run("download is cancelled", func(t *testing.T) {
		chunk := newChunk(0, 99, tempDir)
		downloader := &mockDownloader{block: make(chan struct{})}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		errChan := make(chan error, 1)
		go func() {
			errChan <- chunk.Download(ctx, downloader, false)
		}()

		select {
		case err := <-errChan:
			require.Error(t, err, "Expected an error from cancelled download")
			assert.ErrorIs(t, err, context.DeadlineExceeded, "Error should be context.DeadlineExceeded")
			assert.Equal(t, status.Active, chunk.getStatus(), "Chunk status should remain Active")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Test timed out, indicating a deadlock")
		}
	})

	t.Run("download fails on connection read error", func(t *testing.T) {
		chunk := newChunk(0, 99, tempDir)
		expectedErr := errors.New("network error")
		downloader := &mockDownloader{err: expectedErr}

		err := chunk.Download(context.Background(), downloader, false)
		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
		assert.Equal(t, status.Failed, chunk.getStatus())
	})

	t.Run("download fails on file open error", func(t *testing.T) {
		readOnlyDir := filepath.Join(tempDir, "readonly")
		require.NoError(t, os.Mkdir(readOnlyDir, 0o555)) // Read and execute only

		chunk := newChunk(0, 99, readOnlyDir)
		downloader := &mockDownloader{reader: bytes.NewReader([]byte("data"))}

		err := chunk.Download(context.Background(), downloader, false)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrChunkFileOpenFailed)
		assert.Equal(t, status.Failed, chunk.getStatus())
	})

	t.Run("download fails on file write error", func(t *testing.T) {
		chunk := newChunk(0, 99, tempDir)
		testData := bytes.Repeat([]byte("a"), 100)
		downloader := &mockDownloader{reader: bytes.NewReader(testData)}

		readOnlyDir := filepath.Join(tempDir, "write_test_dir")
		require.NoError(t, os.Mkdir(readOnlyDir, 0o555))
		chunk.TempFilePath = filepath.Join(readOnlyDir, chunk.ID.String())

		err := chunk.Download(context.Background(), downloader, false)
		require.Error(t, err)

		assert.ErrorIs(t, err, ErrChunkFileOpenFailed, "Expected a file open error due to read-only directory")
		assert.Equal(t, status.Failed, chunk.getStatus())
	})

	t.Run("resuming a download", func(t *testing.T) {
		chunk := newChunk(0, 199, tempDir)
		initialData := bytes.Repeat([]byte("a"), 50)
		err := os.WriteFile(chunk.getTempFilePath(), initialData, 0o644)
		require.NoError(t, err)

		chunk.updateDownloaded(50)

		remainingData := bytes.Repeat([]byte("b"), 150)
		downloader := &mockDownloader{reader: bytes.NewReader(remainingData)}

		err = chunk.Download(context.Background(), downloader, false)
		require.NoError(t, err)

		assert.Equal(t, status.Completed, chunk.getStatus())
		assert.Equal(t, int64(200), chunk.getDownloaded())

		finalContent, err := os.ReadFile(chunk.getTempFilePath())
		require.NoError(t, err)

		expectedContent := append(initialData, remainingData...)
		assert.Equal(t, expectedContent, finalContent)
	})

	t.Run("sequential download overwrites and resets progress", func(t *testing.T) {
		chunk := newChunk(0, 199, tempDir)
		initialData := bytes.Repeat([]byte("a"), 50)
		err := os.WriteFile(chunk.getTempFilePath(), initialData, 0o644)
		require.NoError(t, err)
		chunk.updateDownloaded(50)

		fullData := bytes.Repeat([]byte("c"), 200)
		downloader := &mockDownloader{reader: bytes.NewReader(fullData)}

		err = chunk.Download(context.Background(), downloader, true)
		require.NoError(t, err)

		assert.Equal(t, status.Completed, chunk.getStatus())
		assert.Equal(t, int64(200), chunk.getDownloaded())

		finalContent, err := os.ReadFile(chunk.getTempFilePath())
		require.NoError(t, err)
		assert.Equal(t, fullData, finalContent)
	})

	t.Run("download loop handles more data than remaining", func(t *testing.T) {
		chunk := newChunk(0, 49, tempDir)
		initialData := bytes.Repeat([]byte("a"), 30)
		err := os.WriteFile(chunk.getTempFilePath(), initialData, 0o644)
		require.NoError(t, err)
		chunk.updateDownloaded(30)

		extraData := bytes.Repeat([]byte("b"), 30)
		downloader := &mockDownloader{reader: bytes.NewReader(extraData)}

		err = chunk.Download(context.Background(), downloader, false)
		require.NoError(t, err)

		assert.Equal(t, status.Completed, chunk.getStatus())
		assert.Equal(t, int64(50), chunk.getDownloaded())

		fileContent, err := os.ReadFile(chunk.getTempFilePath())
		require.NoError(t, err)
		assert.Len(t, fileContent, 50)
		expectedContent := append(initialData, bytes.Repeat([]byte("b"), 20)...)
		assert.Equal(t, expectedContent, fileContent)
	})
}

func TestChunk_ConcurrentState(t *testing.T) {
	c := newChunk(0, 10000, "/tmp")
	var wg sync.WaitGroup
	numRoutines := 100

	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			c.updateDownloaded(1)
			c.setStatus(status.Active)
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(100), c.getDownloaded())
	assert.Equal(t, status.Active, c.getStatus())
}
