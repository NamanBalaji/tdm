package http_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpdl "github.com/NamanBalaji/tdm/internal/downloaders/http"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "network problem is retryable",
			err:      httpPkg.ErrNetworkProblem,
			expected: true,
		},
		{
			name:     "server problem is retryable",
			err:      httpPkg.ErrServerProblem,
			expected: true,
		},
		{
			name:     "too many requests is retryable",
			err:      httpPkg.ErrTooManyRequests,
			expected: true,
		},
		{
			name:     "timeout is retryable",
			err:      httpPkg.ErrTimeout,
			expected: true,
		},
		{
			name:     "wrapped retryable error is retryable",
			err:      fmt.Errorf("something failed: %w", httpPkg.ErrNetworkProblem),
			expected: true,
		},
		{
			name:     "resource not found is not retryable",
			err:      httpPkg.ErrResourceNotFound,
			expected: false,
		},
		{
			name:     "access denied is not retryable",
			err:      httpPkg.ErrAccessDenied,
			expected: false,
		},
		{
			name:     "generic error is not retryable",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "nil error is not retryable",
			err:      nil,
			expected: false,
		},
		{
			name:     "context canceled is not retryable",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "io EOF is not retryable",
			err:      io.EOF,
			expected: false,
		},
		{
			name:     "unexpected EOF (premature server close) is retryable",
			err:      fmt.Errorf("%w: server closed connection", httpPkg.ErrUnexpectedEOF),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpdl.IsRetryableError(tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	t.Run("exponential growth", func(t *testing.T) {
		base := 1 * time.Second

		b0 := httpdl.CalculateBackoff(0, base)
		b1 := httpdl.CalculateBackoff(1, base)
		b2 := httpdl.CalculateBackoff(2, base)

		// With ±10% jitter, attempt 0 should be ~1s, attempt 1 ~2s, attempt 2 ~4s
		assert.InDelta(t, float64(1*time.Second), float64(b0), float64(150*time.Millisecond))
		assert.InDelta(t, float64(2*time.Second), float64(b1), float64(250*time.Millisecond))
		assert.InDelta(t, float64(4*time.Second), float64(b2), float64(500*time.Millisecond))
	})

	t.Run("capped at 2 minutes", func(t *testing.T) {
		base := 1 * time.Second
		maxBackoff := 2 * time.Minute

		// attempt 10 would be 1024s without cap
		b := httpdl.CalculateBackoff(10, base)
		// With +10% jitter on cap: max possible is 2m + 10% = 2m12s
		assert.LessOrEqual(t, b, maxBackoff+maxBackoff/10+1)
	})

	t.Run("jitter within bounds", func(t *testing.T) {
		base := 1 * time.Second

		for i := range 100 {
			_ = i
			b := httpdl.CalculateBackoff(0, base)
			// ±10% of 1s means [900ms, 1100ms]
			assert.GreaterOrEqual(t, b, 900*time.Millisecond)
			assert.LessOrEqual(t, b, 1100*time.Millisecond)
		}
	})

	t.Run("never returns less than 1ms", func(t *testing.T) {
		b := httpdl.CalculateBackoff(0, time.Millisecond)
		assert.GreaterOrEqual(t, b, time.Millisecond)
	})
}

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		header    string
		wantStart int64
		wantTotal int64
		wantOK    bool
	}{
		{"bytes 0-99/1000", 0, 1000, true},
		{"bytes 500-999/1000", 500, 1000, true},
		{"bytes 500-999/*", 500, -1, true},        // total unknown
		{"bytes 500-999", 500, -1, true},          // total omitted (non-conforming)
		{"bytes 500-999/", 500, -1, true},         // total empty (non-conforming)
		{"bytes 500-999/ 1000 ", 500, 1000, true}, // tolerate whitespace
		{"bytes */1000", 0, 0, false},             // unsatisfied-range form
		{"items 0-99/1000", 0, 0, false},
		{"bytes -5-99/1000", 0, 0, false},
		{"bytes 0-99/abc", 0, 0, false},
		{"bytes 0-99/-1", 0, 0, false},
		{"", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			start, total, ok := httpdl.ParseContentRange(tt.header)
			assert.Equal(t, tt.wantOK, ok)

			if tt.wantOK {
				assert.Equal(t, tt.wantStart, start)
				assert.Equal(t, tt.wantTotal, total)
			}
		})
	}
}

func TestConnectionRead(t *testing.T) {
	t.Run("successful read", func(t *testing.T) {
		body := "hello world"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(body))
		}))
		defer srv.Close()

		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, nil, client, 0, int64(len(body)-1))

		buf := make([]byte, 64)
		n, err := conn.Read(context.Background(), buf)
		// Read may return data + io.EOF simultaneously or data + nil then EOF
		if err != nil && err != io.EOF {
			require.NoError(t, err)
		}
		assert.Equal(t, body, string(buf[:n]))
	})

	t.Run("server returns 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, nil, client, 0, 10)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrResourceNotFound)
	})

	t.Run("server returns 500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, nil, client, 0, 10)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrServerProblem)
	})

	t.Run("custom headers are sent", func(t *testing.T) {
		var receivedHeaders http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header
			w.Header().Set("Content-Range", "bytes 100-200/1000")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("ok"))
		}))
		defer srv.Close()

		headers := map[string]string{
			"Range":      "bytes=100-200",
			"User-Agent": "TestAgent/1.0",
		}
		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, headers, client, 100, 200)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		if err != nil && err != io.EOF {
			require.NoError(t, err)
		}
		assert.Equal(t, "bytes=100-200", receivedHeaders.Get("Range"))
		assert.Equal(t, "TestAgent/1.0", receivedHeaders.Get("User-Agent"))
	})

	t.Run("200 response to a range request is rejected", func(t *testing.T) {
		// A server that ignores the Range header sends bytes from 0; writing
		// them into a chunk at a different offset would corrupt the file.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("whole file from byte zero"))
		}))
		defer srv.Close()

		headers := map[string]string{"Range": "bytes=100-200"}
		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, headers, client, 100, 200)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrRangesNotSupported)
	})

	t.Run("mismatched Content-Range start is rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 0-200/1000")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("data"))
		}))
		defer srv.Close()

		headers := map[string]string{"Range": "bytes=100-200"}
		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, headers, client, 100, 200)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrInvalidContentRange)
	})

	t.Run("missing Content-Range is rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("data"))
		}))
		defer srv.Close()

		headers := map[string]string{"Range": "bytes=100-200"}
		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, headers, client, 100, 200)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrInvalidContentRange)
	})

	t.Run("range response for a different file size is rejected", func(t *testing.T) {
		// The file on the server was replaced by a bigger one after the
		// download was added: the right offset, but of a different file.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 100-200/2000")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("data"))
		}))
		defer srv.Close()

		headers := map[string]string{"Range": "bytes=100-200"}
		conn := httpdl.NewTestConnectionWithTotal(srv.URL, headers, httpPkg.NewClient(0), 100, 200, 1000)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrSizeMismatch)
		assert.False(t, httpdl.IsRetryableError(err), "retrying can't fix a changed remote file")
	})

	t.Run("range response with matching or unknown total is accepted", func(t *testing.T) {
		for _, cr := range []string{"bytes 100-200/1000", "bytes 100-200/*"} {
			t.Run(cr, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Range", cr)
					w.WriteHeader(http.StatusPartialContent)
					w.Write([]byte("data"))
				}))
				defer srv.Close()

				headers := map[string]string{"Range": "bytes=100-200"}
				conn := httpdl.NewTestConnectionWithTotal(srv.URL, headers, httpPkg.NewClient(0), 100, 200, 1000)

				buf := make([]byte, 64)
				n, err := conn.Read(context.Background(), buf)
				if err != nil && err != io.EOF {
					require.NoError(t, err)
				}
				assert.Equal(t, "data", string(buf[:n]))
			})
		}
	})

	t.Run("full response for a different file size is rejected", func(t *testing.T) {
		// No Range header (the server doesn't support ranges): the whole
		// file comes back, and its length must still match.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "2000")
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 2000))
		}))
		defer srv.Close()

		conn := httpdl.NewTestConnectionWithTotal(srv.URL, nil, httpPkg.NewClient(0), 0, 999, 1000)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrSizeMismatch)
	})

	t.Run("lazy connection - not connected until first read", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Write([]byte("data"))
		}))
		defer srv.Close()

		client := httpPkg.NewClient(0)
		conn := httpdl.NewTestConnection(srv.URL, nil, client, 0, 3)
		assert.Equal(t, 0, callCount)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		if err != nil && err != io.EOF {
			require.NoError(t, err)
		}
		assert.Equal(t, 1, callCount)

		// Second read should not reconnect (may return EOF)
		_, _ = conn.Read(context.Background(), buf)
		assert.Equal(t, 1, callCount)
	})
}
