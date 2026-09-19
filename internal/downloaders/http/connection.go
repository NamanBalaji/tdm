package http

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	totalSize int64 // size of the whole file when the download was added; <= 0 = unknown
}

func newConnection(url string, headers map[string]string, client *httpPkg.Client, start, end, totalSize int64) *connection {
	return &connection{
		url:       url,
		headers:   headers,
		client:    client,
		startByte: start,
		endByte:   end,
		totalSize: totalSize,
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

	if err := c.validate(resp); err != nil {
		_ = resp.Body.Close()
		return err
	}

	c.response = resp

	return nil
}

// validate checks that a response carries the bytes this connection asked
// for, of the file this download was started on.
func (c *connection) validate(resp *http.Response) error {
	if _, ranged := c.headers["Range"]; !ranged {
		return c.checkTotal(resp.ContentLength)
	}

	// For a byte range the server MUST answer 206 with a matching
	// Content-Range. A 200 means it ignored the Range header and is sending
	// the whole file from byte 0, which would corrupt this chunk.
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("%w: got status %d for range request", httpPkg.ErrRangesNotSupported, resp.StatusCode)
	}

	cr := resp.Header.Get("Content-Range")

	start, total, ok := parseContentRange(cr)
	if !ok || start != c.startByte {
		return fmt.Errorf("%w: got %q, want start %d", httpPkg.ErrInvalidContentRange, cr, c.startByte)
	}

	return c.checkTotal(total)
}

// checkTotal rejects a response whose total file size differs from the size
// recorded when the download was added: the file on the server was replaced
// (or the server is inconsistent), and merging chunks of two versions would
// silently corrupt the output. Retrying can't fix that, so the error is not
// retryable. Unknown sizes (< 0) pass.
func (c *connection) checkTotal(reported int64) error {
	if c.totalSize <= 0 || reported < 0 || reported == c.totalSize {
		return nil
	}

	return fmt.Errorf("%w (expected %d bytes, server now reports %d); remove and re-add the download to start over",
		httpPkg.ErrSizeMismatch, c.totalSize, reported)
}

// parseContentRange parses a "bytes <start>-<end>/<total>" Content-Range
// header, returning the start offset and the total size. The total is -1
// when the server reports it as unknown ("*") or leaves it out.
func parseContentRange(header string) (int64, int64, bool) {
	rest, ok := strings.CutPrefix(header, "bytes ")
	if !ok {
		return 0, 0, false
	}

	rng, totalStr, hasTotal := strings.Cut(rest, "/")

	startStr, _, ok := strings.Cut(rng, "-")
	if !ok {
		return 0, 0, false
	}

	start, err := strconv.ParseInt(strings.TrimSpace(startStr), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}

	totalStr = strings.TrimSpace(totalStr)
	if !hasTotal || totalStr == "" || totalStr == "*" {
		return start, -1, true
	}

	total, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil || total < 0 {
		return 0, 0, false
	}

	return start, total, true
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
	// A server closing the connection mid-chunk is usually transient; the
	// retry resumes from the last received byte.
	httpPkg.ErrUnexpectedEOF,
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
