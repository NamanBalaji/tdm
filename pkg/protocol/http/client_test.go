// pkg/protocol/http/client_test.go

package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Run("creates client with nil config", func(t *testing.T) {
		client := NewClient(nil)
		assert.NotNil(t, client)
		assert.Implements(t, (*protocol.Protocol)(nil), client)
	})

	t.Run("creates client with custom config", func(t *testing.T) {
		config := &ClientConfig{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
			MaxConnsPerHost:     20,
			MaxRedirects:        5,
			DialTimeout:         5 * time.Second,
		}
		client := NewClient(config)
		assert.NotNil(t, client)
		assert.Implements(t, (*protocol.Protocol)(nil), client)
	})
}

func TestSupportsMethod(t *testing.T) {
	client := NewClient(nil)

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "supports http",
			url:  "http://example.com",
			want: true,
		},
		{
			name: "supports https",
			url:  "https://example.com",
			want: true,
		},
		{
			name: "doesn't support ftp",
			url:  "ftp://example.com",
			want: false,
		},
		{
			name: "doesn't support invalid url",
			url:  "not-a-url",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.Supports(tt.url)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInitialize(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodHead, r.Method)
			w.Header().Set("Content-Length", "1000")
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Accept-Ranges", "bytes")
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		require.NoError(t, err)
		assert.NotNil(t, fileInfo)
		assert.Equal(t, int64(1000), fileInfo.Size)
		assert.Equal(t, "text/plain", fileInfo.ContentType)
		assert.True(t, fileInfo.Resumable)
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		assert.Error(t, err)
		assert.Nil(t, fileInfo)
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(ctx, server.URL, protocol.DownloadOptions{})

		assert.Error(t, err)
		assert.Nil(t, fileInfo)
	})

	t.Run("custom headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "test-value", r.Header.Get("X-Test-Header"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil)
		opts := protocol.DownloadOptions{
			Headers: map[string]string{
				"X-Test-Header": "test-value",
			},
		}
		_, err := client.Initialize(context.Background(), server.URL, opts)
		require.NoError(t, err)
	})

	t.Run("HEAD not allowed falls back to range check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodHead:
				w.WriteHeader(http.StatusMethodNotAllowed)
			case http.MethodGet:
				if r.Header.Get("Range") == "bytes=0-0" {
					w.Header().Set("Content-Range", "bytes 0-0/1000")
					w.Header().Set("Content-Length", "1")
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusPartialContent)
				}
			}
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		require.NoError(t, err)
		assert.Equal(t, int64(1000), fileInfo.Size)
		assert.True(t, fileInfo.Resumable)
	})

	t.Run("server doesn't support range requests", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// Ignore range header and send full response
			w.Header().Set("Content-Length", "1000")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		require.NoError(t, err)
		assert.Equal(t, int64(1000), fileInfo.Size)
		assert.False(t, fileInfo.Resumable)
	})

	t.Run("content disposition filename", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Disposition", `attachment; filename="test.txt"`)
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		require.NoError(t, err)
		assert.Equal(t, "test.txt", fileInfo.Filename)
	})

	t.Run("filename from URL when no content disposition", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL+"/path/testfile.txt", protocol.DownloadOptions{})

		require.NoError(t, err)
		assert.Equal(t, "testfile.txt", fileInfo.Filename)
	})

	t.Run("error handling with proper HTTP error types", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewClient(nil)
		_, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		assert.Error(t, err)
		var httpErr *HTTPError
		assert.True(t, errors.As(err, &httpErr))
		assert.Equal(t, ErrorTypeHTTP, httpErr.Type)
		assert.Equal(t, http.StatusInternalServerError, httpErr.Status)
	})

	t.Run("handles network errors", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.Initialize(context.Background(), "http://invalid.localhost:9999", protocol.DownloadOptions{})

		assert.Error(t, err)
		var httpErr *HTTPError
		assert.True(t, errors.As(err, &httpErr))
		assert.Equal(t, ErrorTypeNetwork, httpErr.Type)
	})

	t.Run("HEAD and Range both fail falls back to normal GET", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodHead:
				w.WriteHeader(http.StatusMethodNotAllowed)
			case http.MethodGet:
				// Always return 200 OK with full content
				w.Header().Set("Content-Length", "1000")
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		client := NewClient(nil)
		fileInfo, err := client.Initialize(context.Background(), server.URL, protocol.DownloadOptions{})

		require.NoError(t, err)
		assert.Equal(t, int64(1000), fileInfo.Size)
		assert.False(t, fileInfo.Resumable)
	})
}

func TestCleanup(t *testing.T) {
	t.Run("cleanup succeeds", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Cleanup()
		assert.NoError(t, err)
	})
}

func TestCreateDownloader(t *testing.T) {
	t.Run("creates downloader", func(t *testing.T) {
		client := NewClient(nil)
		downloader, err := client.CreateDownloader(context.Background(), "http://example.com")

		assert.Error(t, err)
		assert.Nil(t, downloader)
	})
}
