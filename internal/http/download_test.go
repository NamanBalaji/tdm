package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/status"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

// setupDownloadTest creates a temporary directory for download files and returns a cleanup function.
func setupDownloadTest(t *testing.T) (string, func()) {
	t.Helper()
	// Override XDG download dir for tests to avoid cluttering the user's real directory.
	tempHome, err := os.MkdirTemp("", "xdg_home_*")
	require.NoError(t, err)
	t.Setenv("HOME", tempHome)
	xdg.Reload()

	tempDir, err := os.MkdirTemp("", "download_test_*")
	require.NoError(t, err)

	cleanup := func() {
		assert.NoError(t, os.RemoveAll(tempDir))
		assert.NoError(t, os.RemoveAll(tempHome))
	}
	return tempDir, cleanup
}

// mockServer provides a configurable httptest.Server for download tests.
func mockServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestNewDownload(t *testing.T) {
	_, cleanup := setupDownloadTest(t)
	defer cleanup()

	client := httpPkg.NewClient()

	t.Run("successful initialization with range support", func(t *testing.T) {
		server := mockServer(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "1024")
			w.Header().Set("Content-Disposition", `attachment; filename="testfile.zip"`)
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		d, err := NewDownload(context.Background(), server.URL, client, 16, 5)
		require.NoError(t, err)
		require.NotNil(t, d)

		assert.Equal(t, "testfile.zip", d.Filename)
		assert.Equal(t, int64(1024), d.TotalSize)
		assert.True(t, d.SupportsRanges)
		assert.Equal(t, status.Pending, d.getStatus())
		assert.Equal(t, 5, d.Priority)
		assert.NotEmpty(t, d.Chunks)
		assert.True(t, len(d.Chunks) > 1)
	})

	t.Run("successful initialization without range support", func(t *testing.T) {
		server := mockServer(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "2048")
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		d, err := NewDownload(context.Background(), server.URL, client, 16, 5)
		require.NoError(t, err)
		require.NotNil(t, d)

		assert.Equal(t, int64(2048), d.TotalSize)
		assert.False(t, d.SupportsRanges)
		assert.Len(t, d.Chunks, 1, "Should create only one chunk if ranges are not supported")
	})

	t.Run("initialization fails with server error", func(t *testing.T) {
		server := mockServer(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		defer server.Close()

		_, err := NewDownload(context.Background(), server.URL, client, 16, 5)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrResourceNotFound)
	})

	t.Run("fallback to GET when HEAD fails", func(t *testing.T) {
		server := mockServer(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// This server doesn't support ranges on GET, but it works
			w.Header().Set("Content-Length", "500")
			w.WriteHeader(http.StatusOK)
		})
		defer server.Close()

		d, err := NewDownload(context.Background(), server.URL, client, 16, 5)
		require.NoError(t, err)
		require.NotNil(t, d)
		assert.Equal(t, int64(500), d.TotalSize)
		assert.False(t, d.SupportsRanges)
		assert.Len(t, d.Chunks, 1)
	})

	t.Run("fallback to Range GET when HEAD fails", func(t *testing.T) {
		server := mockServer(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-0/1234")
			w.WriteHeader(http.StatusPartialContent)
		})
		defer server.Close()

		d, err := NewDownload(context.Background(), server.URL, client, 16, 5)
		require.NoError(t, err)
		require.NotNil(t, d)
		assert.Equal(t, int64(1234), d.TotalSize)
		assert.True(t, d.SupportsRanges)
		assert.True(t, len(d.Chunks) > 1)
	})
}

func TestDownload_StateAndGetters(t *testing.T) {
	d := &Download{
		Id:             uuid.New(),
		URL:            "http://example.com",
		Filename:       "file.dat",
		TotalSize:      1000,
		Downloaded:     100,
		Status:         status.Active,
		StartTime:      time.Now(),
		EndTime:        time.Now().Add(1 * time.Hour),
		Chunks:         []*Chunk{{ID: uuid.New()}},
		SupportsRanges: true,
		Protocol:       "http",
		Dir:            "/tmp/downloads",
		TempDir:        "/tmp/temp",
		Priority:       8,
	}

	assert.Equal(t, d.Id, d.GetID())
	assert.Equal(t, d.URL, d.getURL())
	assert.Equal(t, d.TempDir, d.getTempDir())
	assert.Equal(t, d.Dir, d.getDir())
	assert.Equal(t, status.Active, d.getStatus())
	assert.Equal(t, int64(1000), d.getTotalSize())
	assert.True(t, d.getSupportsRanges())
	assert.Equal(t, d.Chunks, d.getChunks())
	assert.Equal(t, 8, d.getPriority())

	// Test setters
	d.setStatus(status.Paused)
	assert.Equal(t, status.Paused, d.getStatus())

	d.setDownloaded(500)
	assert.Equal(t, int64(500), d.Downloaded)

	newStartTime := time.Now().Add(-1 * time.Hour)
	d.setStartTime(newStartTime)
	assert.Equal(t, newStartTime, d.StartTime)

	newEndTime := time.Now().Add(2 * time.Hour)
	d.setEndTime(newEndTime)
	assert.Equal(t, newEndTime, d.EndTime)
}

func TestDownload_makeChunks(t *testing.T) {
	t.Run("with range support", func(t *testing.T) {
		d := &Download{TotalSize: 1000, SupportsRanges: true, TempDir: "/tmp"}
		d.makeChunks(10)
		assert.Len(t, d.Chunks, 10)
		assert.Equal(t, int64(0), d.Chunks[0].StartByte)
		assert.Equal(t, int64(99), d.Chunks[0].EndByte)
		assert.Equal(t, int64(900), d.Chunks[9].StartByte)
		assert.Equal(t, int64(999), d.Chunks[9].EndByte)
	})

	t.Run("without range support", func(t *testing.T) {
		d := &Download{TotalSize: 1000, SupportsRanges: false, TempDir: "/tmp"}
		d.makeChunks(10)
		assert.Len(t, d.Chunks, 1)
		assert.Equal(t, int64(0), d.Chunks[0].StartByte)
		assert.Equal(t, int64(999), d.Chunks[0].EndByte)
	})

	t.Run("zero size file", func(t *testing.T) {
		d := &Download{TotalSize: 0, SupportsRanges: true, TempDir: "/tmp"}
		d.makeChunks(10)
		assert.Empty(t, d.Chunks)
	})
}

func TestDownload_getDownloadableChunks(t *testing.T) {
	d := &Download{
		Chunks: []*Chunk{
			{Status: status.Pending},
			{Status: status.Completed},
			{Status: status.Active},
			{Status: status.Failed},
		},
	}
	downloadable := d.getDownloadableChunks()
	assert.Len(t, downloadable, 3)
	for _, c := range downloadable {
		assert.NotEqual(t, status.Completed, c.getStatus(), "Completed chunks should not be downloadable")
	}
}

func TestDownload_MarshalJSON(t *testing.T) {
	id := uuid.New()
	d := &Download{
		Id:        id,
		URL:       "http://url.com",
		Filename:  "file.json",
		TotalSize: 100,
		Status:    status.Paused,
	}
	d.setStatus(status.Paused) // Ensure atomic value is set

	b, err := json.Marshal(d)
	require.NoError(t, err)

	var data map[string]interface{}
	err = json.Unmarshal(b, &data)
	require.NoError(t, err)

	assert.Equal(t, id.String(), data["id"])
	assert.Equal(t, "http://url.com", data["url"])
	assert.Equal(t, "file.json", data["filename"])
	assert.Equal(t, float64(100), data["totalSize"]) // JSON numbers are float64
	assert.Equal(t, float64(status.Paused), data["status"])
}

func TestDownload_Type(t *testing.T) {
	d := &Download{}
	assert.Equal(t, "http", d.Type())
}

func TestDownload_TempDirCreation(t *testing.T) {
	_, cleanup := setupDownloadTest(t)
	defer cleanup()

	client := httpPkg.NewClient()
	server := mockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	})
	defer server.Close()

	d, err := NewDownload(context.Background(), server.URL, client, 4, 1)
	require.NoError(t, err)

	// Check that the temp directory was created
	_, err = os.Stat(d.getTempDir())
	assert.NoError(t, err, "Temp directory should be created by NewDownload")
	assert.True(t, strings.HasPrefix(d.getTempDir(), os.TempDir()), "Temp dir should be in OS temp")
}
