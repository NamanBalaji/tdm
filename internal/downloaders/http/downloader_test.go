package http_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/NamanBalaji/tdm/internal/download"
	httpdl "github.com/NamanBalaji/tdm/internal/downloaders/http"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func TestMakeChunks(t *testing.T) {
	tempDir := "/tmp/test"

	t.Run("nil when totalSize is zero", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 0, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 4)
		assert.Nil(t, chunks)
	})

	t.Run("nil when totalSize is negative", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", -1, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 4)
		assert.Nil(t, chunks)
	})

	t.Run("single chunk when ranges not supported", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 1000, false)
		chunks := httpdl.MakeChunks(meta, tempDir, 4)
		require.Len(t, chunks, 1)
		assert.Equal(t, int64(0), chunks[0].StartByte)
		assert.Equal(t, int64(999), chunks[0].EndByte)
	})

	t.Run("single chunk when file smaller than chunk count", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 2, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 10)
		require.Len(t, chunks, 1)
		assert.Equal(t, int64(0), chunks[0].StartByte)
		assert.Equal(t, int64(1), chunks[0].EndByte)
	})

	t.Run("even split", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 1000, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 4)
		require.Len(t, chunks, 4)

		assert.Equal(t, int64(0), chunks[0].StartByte)
		assert.Equal(t, int64(249), chunks[0].EndByte)

		assert.Equal(t, int64(250), chunks[1].StartByte)
		assert.Equal(t, int64(499), chunks[1].EndByte)

		assert.Equal(t, int64(500), chunks[2].StartByte)
		assert.Equal(t, int64(749), chunks[2].EndByte)

		assert.Equal(t, int64(750), chunks[3].StartByte)
		assert.Equal(t, int64(999), chunks[3].EndByte)
	})

	t.Run("uneven split - last chunk absorbs remainder", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 1001, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 4)

		// No gaps between chunks
		for i := 1; i < len(chunks); i++ {
			assert.Equal(t, chunks[i-1].EndByte+1, chunks[i].StartByte,
				"gap between chunk %d and %d", i-1, i)
		}
		// Last chunk ends at totalSize-1
		assert.Equal(t, int64(1000), chunks[len(chunks)-1].EndByte)
	})

	t.Run("single chunk requested", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 5000, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 1)
		require.Len(t, chunks, 1)
		assert.Equal(t, int64(0), chunks[0].StartByte)
		assert.Equal(t, int64(4999), chunks[0].EndByte)
	})

	t.Run("chunk temp file paths use tempDir", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 1000, true)
		chunks := httpdl.MakeChunks(meta, "/my/temp", 2)
		for _, c := range chunks {
			assert.Equal(t, "/my/temp", filepath.Dir(c.TempFilePath))
		}
	})

	t.Run("no gaps or overlaps in byte ranges", func(t *testing.T) {
		meta := httpdl.NewProbeResult("file.bin", 10007, true)
		chunks := httpdl.MakeChunks(meta, tempDir, 7)

		total := int64(0)
		for i, c := range chunks {
			size := c.EndByte - c.StartByte + 1
			assert.Greater(t, size, int64(0), "chunk %d has zero size", i)
			total += size
		}
		assert.Equal(t, int64(10007), total)
	})
}

func TestDownloadChunk(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		content := "chunk data here"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(content))
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		chunkFile := filepath.Join(tmpDir, "chunk0")

		cfg := &config.HTTPConfig{
			MaxRetries:  3,
			RetryDelay:  10 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.NoError(t, err)
		assert.True(t, chunk.Completed)
		assert.Equal(t, int64(len(content)), chunk.Downloaded)
		assert.Equal(t, int64(len(content)), downloaded.Load())

		data, err := os.ReadFile(chunkFile)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("retries on server error then succeeds", func(t *testing.T) {
		content := "retry success"
		attempts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(content))
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		chunkFile := filepath.Join(tmpDir, "chunk0")

		cfg := &config.HTTPConfig{
			MaxRetries:  3,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.NoError(t, err)
		assert.True(t, chunk.Completed)
		assert.Equal(t, 2, attempts)
	})

	t.Run("non-retryable error fails immediately", func(t *testing.T) {
		attempts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		chunkFile := filepath.Join(tmpDir, "chunk0")

		cfg := &config.HTTPConfig{
			MaxRetries:  5,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      100,
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrResourceNotFound)
		assert.Equal(t, 1, attempts)
		assert.False(t, chunk.Completed)
	})

	t.Run("context cancelled stops retries", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		chunkFile := filepath.Join(tmpDir, "chunk0")

		cfg := &config.HTTPConfig{
			MaxRetries:  10,
			RetryDelay:  100 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      100,
			TempFilePath: chunkFile,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(ctx, chunk, srv.URL, true, &downloaded)
		require.Error(t, err)
		assert.False(t, chunk.Completed)
	})

	t.Run("exhausts retries then returns error", func(t *testing.T) {
		attempts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		chunkFile := filepath.Join(tmpDir, "chunk0")

		cfg := &config.HTTPConfig{
			MaxRetries:  3,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      100,
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.Error(t, err)
		assert.Equal(t, 3, attempts)
		assert.Contains(t, err.Error(), "failed after 3 retries")
	})
}

func TestStart(t *testing.T) {
	t.Run("downloads and merges file successfully", func(t *testing.T) {
		content := "hello world from the test server!"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(content))
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		outDir := t.TempDir()

		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     tmpDir,
			Connections: 2,
			Chunks:      1,
			MaxRetries:  2,
			RetryDelay:  1 * time.Millisecond,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunkFile := filepath.Join(tmpDir, "chunk0")
		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					TempFilePath: chunkFile,
				},
			},
			SupportsRanges: true,
			TempDir:        tmpDir,
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			URL:       srv.URL,
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		var lastDownloaded int64
		err = d.Start(context.Background(), dl, func(downloaded, total int64) {
			lastDownloaded = downloaded
		})
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
		_ = lastDownloaded
	})

	t.Run("all chunks complete returns immediately", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("server should not be called when all chunks are complete")
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		outDir := t.TempDir()

		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     tmpDir,
			Connections: 2,
			Chunks:      1,
			MaxRetries:  2,
			RetryDelay:  1 * time.Millisecond,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:         uuid.New(),
					StartByte:  0,
					EndByte:    99,
					Downloaded: 100,
					Completed:  true,
				},
			},
			SupportsRanges: true,
			TempDir:        tmpDir,
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			URL:       srv.URL,
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: 100,
			State:     stateBytes,
		}

		err = d.Start(context.Background(), dl, func(downloaded, total int64) {})
		require.NoError(t, err)
	})

	t.Run("context cancellation saves state", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusPartialContent)
			// Write a little then stall
			w.Write([]byte("partial"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Hold the connection open until client disconnects
			<-r.Context().Done()
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		outDir := t.TempDir()

		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     tmpDir,
			Connections: 1,
			Chunks:      1,
			MaxRetries:  1,
			RetryDelay:  1 * time.Millisecond,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunkFile := filepath.Join(tmpDir, "chunk0")
		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      999,
					TempFilePath: chunkFile,
				},
			},
			SupportsRanges: true,
			TempDir:        tmpDir,
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			URL:       srv.URL,
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: 1000,
			State:     stateBytes,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err = d.Start(ctx, dl, func(downloaded, total int64) {})
		require.Error(t, err)

		// State should be updated (saved for resume)
		assert.NotEqual(t, stateBytes, dl.State)
	})
}

func TestCanHandle(t *testing.T) {
	t.Run("returns type http", func(t *testing.T) {
		cfg := &config.HTTPConfig{}
		d := httpdl.New(cfg)
		assert.Equal(t, "http", d.Type())
	})
}

func TestInit(t *testing.T) {
	t.Run("initializes download with correct metadata", func(t *testing.T) {
		content := "file content for init test"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.Header().Set("Content-Disposition", `attachment; filename="init-test.dat"`)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		outDir := t.TempDir()
		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     tmpDir,
			Connections: 4,
			Chunks:      2,
			MaxRetries:  3,
			RetryDelay:  time.Second,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		dl, err := d.Init(context.Background(), srv.URL, 5)
		require.NoError(t, err)

		assert.Equal(t, srv.URL, dl.URL)
		assert.Equal(t, "init-test.dat", dl.Filename)
		assert.Equal(t, outDir, dl.Dir)
		assert.Equal(t, download.Pending, dl.Status)
		assert.Equal(t, 5, dl.Priority)
		assert.Equal(t, "http", dl.Type)
		assert.Equal(t, int64(len(content)), dl.TotalSize)
		assert.NotEmpty(t, dl.State)

		// Verify state can be deserialized
		var st httpdl.HttpState
		err = json.Unmarshal(dl.State, &st)
		require.NoError(t, err)
		assert.True(t, st.SupportsRanges)
		assert.Len(t, st.Chunks, 2)
	})

	t.Run("probe failure returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			DownloadDir: t.TempDir(),
			TempDir:     t.TempDir(),
			Chunks:      4,
		}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		_, err := d.Init(context.Background(), srv.URL, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to probe URL")
	})
}

func TestRemove(t *testing.T) {
	t.Run("removes temp dir and output file", func(t *testing.T) {
		tmpDir := t.TempDir()
		outDir := t.TempDir()

		chunkDir := filepath.Join(tmpDir, "dl-chunks")
		require.NoError(t, os.MkdirAll(chunkDir, 0o755))

		outFile := filepath.Join(outDir, "test.bin")
		require.NoError(t, os.WriteFile(outFile, []byte("content"), 0o644))

		st := httpdl.HttpState{
			TempDir: chunkDir,
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:       uuid.New(),
			Filename: "test.bin",
			Dir:      outDir,
			State:    stateBytes,
		}

		cfg := &config.HTTPConfig{}
		d := httpdl.New(cfg)
		err = d.Remove(dl)
		require.NoError(t, err)

		_, err = os.Stat(chunkDir)
		assert.True(t, os.IsNotExist(err))

		_, err = os.Stat(outFile)
		assert.True(t, os.IsNotExist(err))
	})
}
