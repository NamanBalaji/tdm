package http

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"slices"
	"time"

	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

// connection wraps an HTTP response for reading chunk data.
type connection struct {
	url       string
	headers   map[string]string
	client    *httpPkg.Client
	response  *http.Response
	startByte int64
	endByte   int64
}

func newConnection(url string, headers map[string]string, client *httpPkg.Client, start, end int64) *connection {
	return &connection{
		url:       url,
		headers:   headers,
		client:    client,
		startByte: start,
		endByte:   end,
	}
}

func (c *connection) Read(ctx context.Context, p []byte) (int, error) {
	if c.response == nil {
		if err := c.connect(ctx); err != nil {
			return 0, err
		}
	}

	return c.response.Body.Read(p)
}

func (c *connection) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Uses the custom httpPkg.Client which embeds *http.Client with optimized transport
	resp, err := c.client.Do(req)
	if err != nil {
		return httpPkg.ClassifyError(err)
	}

	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return httpPkg.ClassifyHTTPError(resp.StatusCode)
	}

	c.response = resp

	return nil
}

func (c *connection) close() error {
	if c.response != nil {
		return c.response.Body.Close()
	}

	return nil
}

var retryableErrors = []error{
	httpPkg.ErrNetworkProblem,
	httpPkg.ErrServerProblem,
	httpPkg.ErrTooManyRequests,
	httpPkg.ErrTimeout,
}

func isRetryableError(err error) bool {
	return slices.ContainsFunc(retryableErrors, func(sentinel error) bool {
		return errors.Is(err, sentinel)
	})
}

func calculateBackoff(attempt int, baseDelay time.Duration) time.Duration {
	backoff := min(baseDelay*time.Duration(1<<uint(attempt)), 2*time.Minute)

	// Add ±10% jitter
	jitter := float64(backoff) * 0.1
	delta := (rand.Float64()*2 - 1) * jitter

	return max(backoff+time.Duration(delta), time.Millisecond)
}
