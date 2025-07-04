package http

import (
	"context"
	"io"
	"net/http"

	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

type connection struct {
	url       string
	headers   map[string]string
	client    *httpPkg.Client
	response  *http.Response
	startByte int64
	endByte   int64
}

func newConnection(url string, headers map[string]string, client *httpPkg.Client, startByte, endByte int64) *connection {
	return &connection{
		url:       url,
		headers:   headers,
		client:    client,
		startByte: startByte,
		endByte:   endByte,
	}
}

func (c *connection) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, http.NoBody)
	if err != nil {
		return httpPkg.ErrRequestCreation
	}

	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return httpPkg.ClassifyError(err)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return httpPkg.ClassifyHTTPError(resp.StatusCode)
	}

	rangeHeader := req.Header.Get("Range")
	if rangeHeader != "" && resp.StatusCode != http.StatusPartialContent {
		// Server doesn't support ranges but returned 200 OK
		// We need to handle this specially - for example, by skipping data
		// if we're resuming a download
		if c.startByte > 0 {
			// Need to manually skip ahead to the start byte
			// This is inefficient but necessary for servers without range support
			if _, err := io.CopyN(io.Discard, resp.Body, c.startByte); err != nil {
				resp.Body.Close()
				return httpPkg.ErrIOProblem
			}
		}
	}

	c.response = resp

	return nil
}

func (c *connection) Read(ctx context.Context, p []byte) (n int, err error) {
	if c.response == nil {
		err := c.connect(ctx)
		if err != nil {
			return 0, err
		}
	}

	return c.response.Body.Read(p)
}

func (c *connection) close() error {
	if c.response != nil && c.response.Body != nil {
		err := c.response.Body.Close()
		return err
	}

	return nil
}
