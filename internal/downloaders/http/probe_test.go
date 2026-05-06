package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/config"
	httpdl "github.com/NamanBalaji/tdm/internal/downloaders/http"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func TestProbe(t *testing.T) {
	t.Run("HEAD succeeds with range support", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set("Content-Length", "5000")
				w.Header().Set("Content-Disposition", `attachment; filename="test.zip"`)
				w.WriteHeader(http.StatusOK)
				return
			}
			t.Fatalf("unexpected method: %s", r.Method)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, "test.zip", result.Filename)
		assert.Equal(t, int64(5000), result.TotalSize)
		assert.True(t, result.SupportsRanges)
	})

	t.Run("HEAD succeeds without range support", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Length", "3000")
				w.WriteHeader(http.StatusOK)
				return
			}
			t.Fatalf("unexpected method: %s", r.Method)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, int64(3000), result.TotalSize)
		assert.False(t, result.SupportsRanges)
	})

	t.Run("HEAD fails with 405 falls back to Range GET", func(t *testing.T) {
		callOrder := []string{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, r.Method)
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
				w.Header().Set("Content-Range", "bytes 0-0/9999")
				w.Header().Set("Content-Disposition", `attachment; filename="ranged.dat"`)
				w.WriteHeader(http.StatusPartialContent)
				w.Write([]byte("x"))
				return
			}
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, "ranged.dat", result.Filename)
		assert.Equal(t, int64(9999), result.TotalSize)
		assert.True(t, result.SupportsRanges)
		assert.Equal(t, []string{"HEAD", "GET"}, callOrder)
	})

	t.Run("HEAD and Range GET both fail falls back to regular GET", func(t *testing.T) {
		callOrder := []string{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, r.Method+":"+r.Header.Get("Range"))
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
				// Range not supported - return 416
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Length", "1234")
				w.WriteHeader(http.StatusOK)
				return
			}
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, int64(1234), result.TotalSize)
		assert.False(t, result.SupportsRanges)
		assert.Len(t, callOrder, 3)
	})

	t.Run("HEAD fails with non-fallback error returns error immediately", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		_, err := d.TestProbe(context.Background(), srv.URL)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrAccessDenied)
	})

	t.Run("Range GET parses Content-Range header correctly", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-0/123456789")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("x"))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, int64(123456789), result.TotalSize)
	})

	t.Run("Range GET with missing Content-Range returns zero size", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			// Return 206 without Content-Range header
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("x"))
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.TotalSize)
		assert.True(t, result.SupportsRanges)
	})

	t.Run("filename extracted from URL path when no Content-Disposition", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cfg := &config.HTTPConfig{}
		client := httpPkg.NewClient()
		d := httpdl.NewDownloaderWithClient(cfg, client)

		result, err := d.TestProbe(context.Background(), srv.URL+"/path/to/myfile.tar.gz")
		require.NoError(t, err)
		assert.Equal(t, "myfile.tar.gz", result.Filename)
	})
}
