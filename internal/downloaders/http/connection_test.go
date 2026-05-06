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

func TestConnectionRead(t *testing.T) {
	t.Run("successful read", func(t *testing.T) {
		body := "hello world"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(body))
		}))
		defer srv.Close()

		client := httpPkg.NewClient()
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

		client := httpPkg.NewClient()
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

		client := httpPkg.NewClient()
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
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}))
		defer srv.Close()

		headers := map[string]string{
			"Range":      "bytes=100-200",
			"User-Agent": "TestAgent/1.0",
		}
		client := httpPkg.NewClient()
		conn := httpdl.NewTestConnection(srv.URL, headers, client, 100, 200)

		buf := make([]byte, 64)
		_, err := conn.Read(context.Background(), buf)
		if err != nil && err != io.EOF {
			require.NoError(t, err)
		}
		assert.Equal(t, "bytes=100-200", receivedHeaders.Get("Range"))
		assert.Equal(t, "TestAgent/1.0", receivedHeaders.Get("User-Agent"))
	})

	t.Run("lazy connection - not connected until first read", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Write([]byte("data"))
		}))
		defer srv.Close()

		client := httpPkg.NewClient()
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
