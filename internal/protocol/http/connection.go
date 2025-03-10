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

func (c *Connection) Connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

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

func (c *Connection) Read(p []byte) (n int, err error) {
	if !c.initialized || c.resp == nil {
		if err := c.Connect(); err != nil {
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

func (c *Connection) Reset() error {
	if c.resp != nil {
		c.resp.Body.Close()
		c.resp = nil
	}
	c.initialized = false

	// Reconnect
	return c.Connect()
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
