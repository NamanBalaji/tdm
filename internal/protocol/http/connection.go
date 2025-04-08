package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Connection implements the Connection interface for HTTP
type Connection struct {
	url         string
	headers     map[string]string
	client      *http.Client
	resp        *http.Response
	startByte   int64
	endByte     int64
	timeout     time.Duration
	initialized bool
}

func NewConnection(url string, headers map[string]string, client *http.Client,
	startByte, endByte int64) *Connection {
	return &Connection{
		url:       url,
		headers:   copyHeaders(headers),
		client:    client,
		startByte: startByte,
		endByte:   endByte,
		timeout:   30 * time.Second,
	}

}

func (c *Connection) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return fmt.Errorf("server returned error status: %s", resp.Status)
	}

	rangeHeader := req.Header.Get("Range")
	if rangeHeader != "" && resp.StatusCode != 206 {
		// Server doesn't support ranges but returned 200 OK
		// We need to handle this specially - for example by skipping data
		// if we're resuming a download
		if c.startByte > 0 {
			// Need to manually skip ahead to the start byte
			// This is inefficient but necessary for servers without range support
			if _, err := io.CopyN(io.Discard, resp.Body, c.startByte); err != nil {
				resp.Body.Close()
				return fmt.Errorf("failed to skip to start position: %w", err)
			}
		}
	}

	c.resp = resp
	c.initialized = true

	return nil
}

func (c *Connection) Read(ctx context.Context, p []byte) (n int, err error) {
	if !c.initialized || c.resp == nil {
		if err := c.Connect(ctx); err != nil {
			return 0, err
		}
	}

	// Read from the response body
	return c.resp.Body.Read(p)
}

func (c *Connection) Close() error {
	if c.resp != nil && c.resp.Body != nil {
		err := c.resp.Body.Close()
		c.resp = nil
		c.initialized = false

		return err
	}
	return nil
}

func (c *Connection) IsAlive() bool {
	return c.initialized && c.resp != nil
}

func (c *Connection) Reset(ctx context.Context) error {
	if c.resp != nil {
		c.resp.Body.Close()
		c.resp = nil
	}
	c.initialized = false

	// Reconnect
	return c.Connect(ctx)
}

func (c *Connection) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *Connection) GetURL() string {
	return c.url
}

func (c *Connection) GetHeaders() map[string]string {
	return c.headers
}

// SetHeader sets a specific header value
func (c *Connection) SetHeader(key, value string) {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
}

func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	headerCopy := make(map[string]string, len(headers))
	for k, v := range headers {
		headerCopy[k] = v
	}
	return headerCopy
}
