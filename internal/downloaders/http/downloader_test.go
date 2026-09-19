package http_test

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// parseRangeStart extracts the start offset from a "bytes=start-end" header.
func parseRangeStart(t *testing.T, header string) int {
	t.Helper()

	rest, ok := strings.CutPrefix(header, "bytes=")
	require.True(t, ok, "expected a Range header, got %q", header)

	startStr, _, _ := strings.Cut(rest, "-")
	start, err := strconv.Atoi(startStr)
	require.NoError(t, err)

	return start
}

func TestDownloadChunk(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		content := "chunk data here"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
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
		client := httpPkg.NewClient(0)
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
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
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
		client := httpPkg.NewClient(0)
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

	t.Run("premature EOF is not treated as success", func(t *testing.T) {
		content := "0123456789abcdefghij"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Promise the full range but deliver nothing, then kill the
			// connection — the old code marked the chunk Completed anyway.
			start := parseRangeStart(t, r.Header.Get("Range"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.Header().Set("Content-Length", strconv.Itoa(len(content)-start))
			w.WriteHeader(http.StatusPartialContent)

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
				}
			}
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  2,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient(0)
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: filepath.Join(t.TempDir(), "chunk0"),
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrUnexpectedEOF)
		assert.False(t, chunk.Completed)
	})

	t.Run("premature EOF is retried and resumes from offset", func(t *testing.T) {
		content := "0123456789abcdefghij"

		var attempts atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := parseRangeStart(t, r.Header.Get("Range"))
			body := content[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)

			if attempts.Add(1) == 1 {
				// First attempt: truncate mid-body.
				w.Write([]byte(body[:5]))

				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}

				if hj, ok := w.(http.Hijacker); ok {
					if conn, _, err := hj.Hijack(); err == nil {
						conn.Close()
					}
				}

				return
			}

			w.Write([]byte(body))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  3,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		client := httpPkg.NewClient(0)
		d := httpdl.NewDownloaderWithClient(cfg, client)

		chunkFile := filepath.Join(t.TempDir(), "chunk0")
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
		assert.Equal(t, int32(2), attempts.Load())

		data, err := os.ReadFile(chunkFile)
		require.NoError(t, err)
		assert.Equal(t, content, string(data), "resumed chunk must be byte-identical")
	})

	t.Run("mid-body TCP reset is retried", func(t *testing.T) {
		content := "0123456789abcdefghij"

		var attempts atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := parseRangeStart(t, r.Header.Get("Range"))
			body := content[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)

			if attempts.Add(1) == 1 {
				// Write half, then abort with an RST (not a clean FIN):
				// SetLinger(0) discards unsent data and resets on Close.
				w.Write([]byte(body[:len(body)/2]))

				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}

				if hj, ok := w.(http.Hijacker); ok {
					if conn, _, err := hj.Hijack(); err == nil {
						if tcp, ok := conn.(*net.TCPConn); ok {
							_ = tcp.SetLinger(0)
						}

						conn.Close()
					}
				}

				return
			}

			w.Write([]byte(body))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  3,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		chunkFile := filepath.Join(t.TempDir(), "chunk0")
		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.NoError(t, err, "a transient connection reset must be retried, not fail the chunk")
		assert.True(t, chunk.Completed)

		data, err := os.ReadFile(chunkFile)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("retry budget resets while the transfer makes progress", func(t *testing.T) {
		content := "0123456789abcdefghij" // 20 bytes

		var attempts atomic.Int32

		// Server caps every connection at 5 bytes, then closes early — a
		// fixed retry budget would fail this chunk despite steady progress.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)

			start := parseRangeStart(t, r.Header.Get("Range"))
			body := content[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(body[:min(5, len(body))]))

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
				}
			}
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  2, // fewer than the 4 connections the chunk needs
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		chunkFile := filepath.Join(t.TempDir(), "chunk0")
		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, true, &downloaded)
		require.NoError(t, err)
		assert.Equal(t, int32(4), attempts.Load())

		data, err := os.ReadFile(chunkFile)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("non-range downloads exhaust retries despite varying truncation", func(t *testing.T) {
		content := "0123456789abcdefghij"

		var attempts atomic.Int32

		// A non-range server that truncates at alternating offsets. Every
		// attempt restarts from byte 0, so "progress" between attempts is
		// illusory — the retry budget must still run out (the buggy version
		// looped forever here).
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := attempts.Add(1)
			cut := 6
			if n%2 == 0 {
				cut = 12
			}

			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(content[:cut]))

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
				}
			}
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  2,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: filepath.Join(t.TempDir(), "chunk0"),
		}

		var downloaded atomic.Int64

		done := make(chan error, 1)
		go func() {
			done <- d.TestDownloadChunk(context.Background(), chunk, srv.URL, false, &downloaded)
		}()

		select {
		case err := <-done:
			require.Error(t, err)
			assert.Equal(t, int32(2), attempts.Load())
			assert.False(t, chunk.Completed)
		case <-time.After(10 * time.Second):
			t.Fatal("retry loop did not terminate (infinite retry regression)")
		}
	})

	t.Run("MaxRetries zero still makes one attempt", func(t *testing.T) {
		content := "single shot"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(content))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  0,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		chunkFile := filepath.Join(t.TempDir(), "chunk0")
		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      int64(len(content) - 1),
			TempFilePath: chunkFile,
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunk(context.Background(), chunk, srv.URL, false, &downloaded)
		require.NoError(t, err, "MaxRetries=0 must mean one attempt, not zero")
		assert.True(t, chunk.Completed)
	})

	t.Run("remote size change fails without retrying or writing", func(t *testing.T) {
		var attempts atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.Header().Set("Content-Range", "bytes 0-9/2000")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("0123456789"))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			MaxRetries:  3,
			RetryDelay:  1 * time.Millisecond,
			Connections: 1,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		chunk := &httpdl.ChunkState{
			ID:           uuid.New(),
			StartByte:    0,
			EndByte:      9,
			TempFilePath: filepath.Join(t.TempDir(), "chunk0"),
		}

		var downloaded atomic.Int64
		err := d.TestDownloadChunkWithTotal(context.Background(), chunk, srv.URL, true, 1000, &downloaded)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrSizeMismatch)
		assert.Equal(t, int32(1), attempts.Load())
		assert.False(t, chunk.Completed)
		assert.Equal(t, int64(0), chunk.Downloaded, "no bytes of the other file may be written")
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
		client := httpPkg.NewClient(0)
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
		client := httpPkg.NewClient(0)
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
		client := httpPkg.NewClient(0)
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
			// ServeContent answers Range requests with a proper 206 + Content-Range.
			http.ServeContent(w, r, "output.txt", time.Time{}, strings.NewReader(content))
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
		client := httpPkg.NewClient(0)
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
		err = d.Start(context.Background(), dl, func(downloaded, total int64, state jsontext.Value) {
			lastDownloaded = downloaded
		})
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
		_ = lastDownloaded
	})

	t.Run("all chunks complete merges without hitting the server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("server should not be called when all chunks are complete")
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
		client := httpPkg.NewClient(0)
		d := httpdl.NewDownloaderWithClient(cfg, client)

		// Simulate a crash after the chunks finished but before the merge:
		// the chunk file exists on disk, no final file yet.
		content := strings.Repeat("x", 100)
		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      99,
					Downloaded:   100,
					Completed:    true,
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
			TotalSize: 100,
			State:     stateBytes,
		}

		err = d.Start(context.Background(), dl, func(downloaded, total int64, state jsontext.Value) {})
		require.NoError(t, err)

		// The resumed Start must still produce the final file (bug: resume
		// used to return early and skip the merge entirely).
		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("resume clamps persisted progress to what reached disk", func(t *testing.T) {
		content := "0123456789abcdefghij"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "output.txt", time.Time{}, strings.NewReader(content))
		}))
		defer srv.Close()

		tmpDir := t.TempDir()
		outDir := t.TempDir()

		// Power-loss shape: the fsynced checkpoint says 10 bytes, but only
		// 5 actually reached the chunk file. Trusting the counter would
		// seek past EOF and zero-fill bytes 5..9.
		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content[:5]), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   10,
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

		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     tmpDir,
			Connections: 1,
			Chunks:      1,
			MaxRetries:  3,
			RetryDelay:  time.Millisecond,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, content, string(data), "resumed file must contain no zero-filled holes")
	})

	t.Run("resume after wiped temp dir re-downloads instead of corrupting", func(t *testing.T) {
		content := "0123456789abcdefghij"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "output.txt", time.Time{}, strings.NewReader(content))
		}))
		defer srv.Close()

		outDir := t.TempDir()
		// Reboot shape: /tmp was wiped, the persisted state was not.
		goneTmpDir := filepath.Join(t.TempDir(), "wiped")

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   15,
					TempFilePath: filepath.Join(goneTmpDir, "chunk0"),
				},
			},
			SupportsRanges: true,
			TempDir:        goneTmpDir,
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

		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     goneTmpDir,
			Connections: 1,
			Chunks:      1,
			MaxRetries:  3,
			RetryDelay:  time.Millisecond,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("crash between rename and temp cleanup does not duplicate the file", func(t *testing.T) {
		outDir := t.TempDir()
		tmpDir := t.TempDir()

		content := strings.Repeat("z", 64)

		// Crash shape: merge renamed the output into place, but the temp
		// dir (with all chunk files) survived and the Completed status was
		// never persisted.
		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.txt"), []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
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
			URL:       "http://unused.invalid",
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		cfg := &config.HTTPConfig{DownloadDir: outDir, TempDir: tmpDir, Connections: 1, Chunks: 1, MaxRetries: 1, RetryDelay: time.Millisecond}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)
		assert.Equal(t, "output.txt", dl.Filename)

		// The already-merged output must be adopted — not merged again into
		// output.1.txt.
		_, err = os.Stat(filepath.Join(outDir, "output.1.txt"))
		assert.True(t, os.IsNotExist(err))

		_, err = os.Stat(tmpDir)
		assert.True(t, os.IsNotExist(err), "leftover temp dir should be cleaned up")
	})

	t.Run("crash after collision rename recovers the renamed output", func(t *testing.T) {
		outDir := t.TempDir()

		content := strings.Repeat("q", 64)

		// Crash shape: the user's own output.txt forced merge to rename to
		// output.1.txt; the temp dir was removed, but the new filename
		// never reached the store.
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.txt"), []byte("user's own file"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.1.txt"), []byte(content), 0o644))

		goneTmpDir := filepath.Join(t.TempDir(), "gone")
		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
					TempFilePath: filepath.Join(goneTmpDir, "chunk0"),
				},
			},
			SupportsRanges: true,
			TempDir:        goneTmpDir,
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			URL:       "http://unused.invalid",
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		cfg := &config.HTTPConfig{DownloadDir: outDir, TempDir: goneTmpDir, Connections: 1, Chunks: 1, MaxRetries: 1, RetryDelay: time.Millisecond}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)
		assert.Equal(t, "output.1.txt", dl.Filename, "recovery must adopt the collision-renamed output")

		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, "user's own file", string(data))
	})

	t.Run("recovery prefers the collision-renamed output over an identical base-name file", func(t *testing.T) {
		outDir := t.TempDir()
		tmpDir := t.TempDir()

		content := strings.Repeat("d", 64)

		// Double-download shape: an earlier download of the same URL owns
		// output.bin (identical bytes); THIS download's merge collision-
		// renamed its output to output.1.bin, then crashed before the new
		// name persisted. Recovery must adopt output.1.bin — adopting
		// output.bin would bind this record to the other download's file
		// (and a later Remove would delete it).
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.bin"), []byte(content), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.1.bin"), []byte(content), 0o644))

		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
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
			URL:       "http://unused.invalid",
			Filename:  "output.bin",
			Dir:       outDir,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		cfg := &config.HTTPConfig{DownloadDir: outDir, TempDir: tmpDir, Connections: 1, Chunks: 1, MaxRetries: 1, RetryDelay: time.Millisecond}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)
		assert.Equal(t, "output.1.bin", dl.Filename)

		// Both files must still exist.
		_, err = os.Stat(filepath.Join(outDir, "output.bin"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(outDir, "output.1.bin"))
		assert.NoError(t, err)
	})

	t.Run("truncated chunk temps cannot veto a valid merged output", func(t *testing.T) {
		outDir := t.TempDir()
		tmpDir := t.TempDir()

		content := strings.Repeat("p", 64)

		// Power-loss shape: the merged output was fsynced, but a chunk temp
		// lost most of its (matching) bytes from the page cache. The short
		// temp proves nothing — recovery must still adopt the output
		// instead of re-merging into a duplicate.
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.bin"), []byte(content), 0o644))

		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content[:3]), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
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
			URL:       "http://unused.invalid",
			Filename:  "output.bin",
			Dir:       outDir,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		cfg := &config.HTTPConfig{DownloadDir: outDir, TempDir: tmpDir, Connections: 1, Chunks: 1, MaxRetries: 1, RetryDelay: time.Millisecond}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)
		assert.Equal(t, "output.bin", dl.Filename)

		_, err = os.Stat(filepath.Join(outDir, "output.1.bin"))
		assert.True(t, os.IsNotExist(err), "must not re-merge into a duplicate")
	})

	t.Run("same-size unrelated file is not mistaken for the merged output", func(t *testing.T) {
		outDir := t.TempDir()
		tmpDir := t.TempDir()

		content := strings.Repeat("a", 64)
		unrelated := strings.Repeat("b", 64) // same size, different bytes

		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.txt"), []byte(unrelated), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
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
			URL:       "http://unused.invalid",
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		cfg := &config.HTTPConfig{DownloadDir: outDir, TempDir: tmpDir, Connections: 1, Chunks: 1, MaxRetries: 1, RetryDelay: time.Millisecond}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.NoError(t, err)
		assert.Equal(t, "output.1.txt", dl.Filename, "content mismatch must force a real merge with collision rename")

		data, err := os.ReadFile(filepath.Join(outDir, "output.txt"))
		require.NoError(t, err)
		assert.Equal(t, unrelated, string(data), "the user's file must be untouched")

		data, err = os.ReadFile(filepath.Join(outDir, "output.1.txt"))
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("all complete and already merged is a no-op", func(t *testing.T) {
		outDir := t.TempDir()

		cfg := &config.HTTPConfig{
			DownloadDir: outDir,
			TempDir:     t.TempDir(),
			Connections: 2,
			Chunks:      1,
			MaxRetries:  2,
			RetryDelay:  1 * time.Millisecond,
		}
		client := httpPkg.NewClient(0)
		d := httpdl.NewDownloaderWithClient(cfg, client)

		content := strings.Repeat("y", 50)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.txt"), []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:         uuid.New(),
					StartByte:  0,
					EndByte:    49,
					Downloaded: 50,
					Completed:  true,
				},
			},
			SupportsRanges: true,
			TempDir:        filepath.Join(cfg.TempDir, "gone"), // already cleaned up
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			URL:       "http://unused.invalid",
			Filename:  "output.txt",
			Dir:       outDir,
			TotalSize: 50,
			State:     stateBytes,
		}

		err = d.Start(context.Background(), dl, func(downloaded, total int64, state jsontext.Value) {})
		require.NoError(t, err)

		// No duplicate produced.
		_, err = os.Stat(filepath.Join(outDir, "output.1.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("remote file replaced by a different size fails instead of mixing versions", func(t *testing.T) {
		var current atomic.Pointer[string]

		original := strings.Repeat("a", 1000)
		current.Store(&original)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "file.bin", time.Time{}, strings.NewReader(*current.Load()))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			DownloadDir: t.TempDir(),
			TempDir:     t.TempDir(),
			Connections: 2,
			Chunks:      4,
			MaxRetries:  3,
			RetryDelay:  time.Millisecond,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		dl, err := d.Init(context.Background(), srv.URL+"/file.bin", 5)
		require.NoError(t, err)
		require.Equal(t, int64(1000), dl.TotalSize)

		// The file on the server is replaced before the download starts.
		replaced := strings.Repeat("b", 2000)
		current.Store(&replaced)

		err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrSizeMismatch)

		entries, err := os.ReadDir(cfg.DownloadDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "no output may be assembled from two versions of the file")
	})

	t.Run("context cancellation saves state", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 0-999/1000")
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
		client := httpPkg.NewClient(0)
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

		err = d.Start(ctx, dl, func(downloaded, total int64, state jsontext.Value) {})
		require.Error(t, err)

		// State should be updated (saved for resume)
		assert.NotEqual(t, stateBytes, dl.State)
	})
}

// End-to-end: probe → multi-chunk ranged download → merge → cleanup.
func TestInitAndStartEndToEnd(t *testing.T) {
	content := make([]byte, 1<<20)
	for i := range content {
		content[i] = byte(i % 251)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "big.bin", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()

	cfg := &config.HTTPConfig{
		DownloadDir: t.TempDir(),
		TempDir:     t.TempDir(),
		Connections: 4,
		Chunks:      8,
		MaxRetries:  3,
		RetryDelay:  time.Millisecond,
	}
	d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

	dl, err := d.Init(context.Background(), srv.URL+"/big.bin", 5)
	require.NoError(t, err)
	assert.Equal(t, "big.bin", dl.Filename)
	assert.Equal(t, int64(len(content)), dl.TotalSize)

	err = d.Start(context.Background(), dl, func(_, _ int64, _ jsontext.Value) {})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(cfg.DownloadDir, dl.Filename))
	require.NoError(t, err)
	require.True(t, bytes.Equal(content, data), "downloaded file must be byte-identical")

	// Per-download temp dir is cleaned up after the merge.
	entries, err := os.ReadDir(cfg.TempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
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
		client := httpPkg.NewClient(0)
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
		client := httpPkg.NewClient(0)
		d := httpdl.NewDownloaderWithClient(cfg, client)

		_, err := d.Init(context.Background(), srv.URL, 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to probe URL")
	})

	t.Run("torrent content type is handed off to the torrent downloader", func(t *testing.T) {
		for _, contentType := range []string{
			"application/x-bittorrent",
			"application/torrent",
			"application/x-bittorrent; charset=binary",
		} {
			t.Run(contentType, func(t *testing.T) {
				// A tracker-style link: nothing in the URL says "torrent".
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", contentType)
					w.Header().Set("Content-Length", "1234")
					w.WriteHeader(http.StatusOK)
				}))
				defer srv.Close()

				cfg := &config.HTTPConfig{
					DownloadDir: t.TempDir(),
					TempDir:     t.TempDir(),
					Chunks:      4,
				}
				d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

				dl, err := d.Init(context.Background(), srv.URL+"/download.php?id=123", 5)
				require.Error(t, err)
				assert.Nil(t, dl)

				handoff, ok := errors.AsType[*download.HandoffError](err)
				require.True(t, ok, "want a HandoffError, got %v", err)
				assert.Equal(t, "torrent", handoff.Type)

				// Nothing may be left behind for a download that was handed off.
				entries, err := os.ReadDir(cfg.TempDir)
				require.NoError(t, err)
				assert.Empty(t, entries)
			})
		}
	})

	t.Run("other content types are downloaded normally", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "1234")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{
			DownloadDir: t.TempDir(),
			TempDir:     t.TempDir(),
			Chunks:      4,
		}
		d := httpdl.NewDownloaderWithClient(cfg, httpPkg.NewClient(0))

		dl, err := d.Init(context.Background(), srv.URL+"/download.php?id=123", 5)
		require.NoError(t, err)
		assert.Equal(t, "http", dl.Type)
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
			Status:   download.Completed,
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

	t.Run("incomplete download leaves a same-named user file alone", func(t *testing.T) {
		outDir := t.TempDir()

		// tdm never wrote this file — the download at 10% has only chunks.
		userFile := filepath.Join(outDir, "report.pdf")
		require.NoError(t, os.WriteFile(userFile, []byte("precious"), 0o644))

		st := httpdl.HttpState{TempDir: filepath.Join(t.TempDir(), "chunks")}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:       uuid.New(),
			Filename: "report.pdf",
			Dir:      outDir,
			Status:   download.Cancelled,
			State:    stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		data, err := os.ReadFile(userFile)
		require.NoError(t, err)
		assert.Equal(t, "precious", string(data))
	})

	t.Run("removes own orphaned part files, leaves other downloads' parts", func(t *testing.T) {
		outDir := t.TempDir()

		id := uuid.New()
		otherID := uuid.New()

		// This download's crash-orphaned part vs a same-named sibling
		// download's (possibly live) part — only the former may be deleted.
		orphan := filepath.Join(outDir, httpdl.PartPrefix("movie.mkv", id)+"123456.part")
		sibling := filepath.Join(outDir, httpdl.PartPrefix("movie.mkv", otherID)+"654321.part")
		require.NoError(t, os.WriteFile(orphan, []byte("half-merged"), 0o644))
		require.NoError(t, os.WriteFile(sibling, []byte("live"), 0o644))

		st := httpdl.HttpState{TempDir: filepath.Join(t.TempDir(), "chunks")}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:       id,
			Filename: "movie.mkv",
			Dir:      outDir,
			Status:   download.Cancelled,
			State:    stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		_, err = os.Stat(orphan)
		assert.True(t, os.IsNotExist(err), "own orphaned part must be removed")

		_, err = os.Stat(sibling)
		assert.NoError(t, err, "another download's part must survive")
	})

	t.Run("part cleanup works with glob metacharacters in the filename", func(t *testing.T) {
		outDir := t.TempDir()

		id := uuid.New()
		siblingID := uuid.New()

		// "[1]" is a glob character class — filepath.Glob would match the
		// WRONG files (report1.pdf...) and miss these.
		orphan := filepath.Join(outDir, httpdl.PartPrefix("report[1].pdf", id)+"111.part")
		sibling := filepath.Join(outDir, httpdl.PartPrefix("report1.pdf", siblingID)+"222.part")
		require.NoError(t, os.WriteFile(orphan, []byte("x"), 0o644))
		require.NoError(t, os.WriteFile(sibling, []byte("y"), 0o644))

		st := httpdl.HttpState{TempDir: filepath.Join(t.TempDir(), "chunks")}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:       id,
			Filename: "report[1].pdf",
			Dir:      outDir,
			Status:   download.Cancelled,
			State:    stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		_, err = os.Stat(orphan)
		assert.True(t, os.IsNotExist(err), "own bracket-named orphan must be removed")

		_, err = os.Stat(sibling)
		assert.NoError(t, err, "differently-named download's part must survive")
	})

	t.Run("remove verifies against chunk temps BEFORE deleting them", func(t *testing.T) {
		outDir := t.TempDir()
		tmpDir := t.TempDir()

		// Pause-during-merge shape: chunks complete, temps intact, no merged
		// output — but the user has their OWN same-name, same-size file with
		// different bytes. Remove must compare against the temps (before
		// destroying them) and leave the user's file alone.
		content := strings.Repeat("t", 64)
		userContent := strings.Repeat("u", 64)

		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, []byte(content), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.bin"), []byte(userContent), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
					TempFilePath: chunkFile,
				},
			},
			TempDir: tmpDir,
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			Filename:  "output.bin",
			Dir:       outDir,
			Status:    download.Paused,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		data, err := os.ReadFile(filepath.Join(outDir, "output.bin"))
		require.NoError(t, err)
		assert.Equal(t, userContent, string(data), "user's same-size file must survive the remove")

		_, err = os.Stat(tmpDir)
		assert.True(t, os.IsNotExist(err), "temp dir must still be cleaned up")
	})

	t.Run("remove skips deletion when several identical candidates are ambiguous", func(t *testing.T) {
		outDir := t.TempDir()

		// Same URL downloaded twice: two byte-identical outputs. This
		// record can't prove which one it owns, so it must delete neither.
		content := strings.Repeat("i", 64)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.bin"), []byte(content), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.1.bin"), []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
					TempFilePath: filepath.Join(t.TempDir(), "gone", "chunk0"),
				},
			},
			TempDir: filepath.Join(t.TempDir(), "gone"),
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			Filename:  "output.bin",
			Dir:       outDir,
			Status:    download.Paused,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		_, err = os.Stat(filepath.Join(outDir, "output.bin"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(outDir, "output.1.bin"))
		assert.NoError(t, err)
	})

	t.Run("remove never deletes a collision-named file it did not record", func(t *testing.T) {
		outDir := t.TempDir()

		// This record never merged (crashed post-chunks, pre-merge; temps
		// since gone). The only size-matching file is output.1.bin — which
		// belongs to ANOTHER record (byte-identical output of the same
		// URL). Remove must not touch it: the sole deletable name is the
		// record's own recorded filename.
		content := strings.Repeat("f", 64)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.1.bin"), []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					Downloaded:   int64(len(content)),
					Completed:    true,
					TempFilePath: filepath.Join(t.TempDir(), "gone", "chunk0"),
				},
			},
			TempDir: filepath.Join(t.TempDir(), "gone"),
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			Filename:  "output.bin", // pre-merge name; .1 was never recorded
			Dir:       outDir,
			Status:    download.Paused,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		_, err = os.Stat(filepath.Join(outDir, "output.1.bin"))
		assert.NoError(t, err, "another record's collision-renamed output must survive")
	})

	t.Run("crash-window remove deletes the verified merged output", func(t *testing.T) {
		outDir := t.TempDir()

		// Crash shape: merge finished but Completed never persisted; on
		// restart the record is Paused. Removing it must still delete the
		// output tdm created (verified by size), not leak it.
		content := strings.Repeat("m", 64)
		require.NoError(t, os.WriteFile(filepath.Join(outDir, "output.bin"), []byte(content), 0o644))

		st := httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:         uuid.New(),
					StartByte:  0,
					EndByte:    int64(len(content) - 1),
					Downloaded: int64(len(content)),
					Completed:  true,
					// temp dir already cleaned by finalize
					TempFilePath: filepath.Join(t.TempDir(), "gone", "chunk0"),
				},
			},
			TempDir: filepath.Join(t.TempDir(), "gone"),
		}
		stateBytes, err := json.Marshal(&st)
		require.NoError(t, err)

		dl := &download.Download{
			ID:        uuid.New(),
			Filename:  "output.bin",
			Dir:       outDir,
			Status:    download.Paused,
			TotalSize: int64(len(content)),
			State:     stateBytes,
		}

		d := httpdl.New(&config.HTTPConfig{})
		require.NoError(t, d.Remove(dl))

		_, err = os.Stat(filepath.Join(outDir, "output.bin"))
		assert.True(t, os.IsNotExist(err), "the merged output must not be leaked")
	})
}
