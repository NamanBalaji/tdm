package http

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultIdleTimeout    = 90 * time.Second
	keepAlivePeriod       = 30 * time.Second
	maxIdleConns          = 100
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
	maxConnsPerHost       = 16

	DefaultUserAgent = "TDM/1.0"

	defaultDownloadName = "download"
)

type Client struct {
	*http.Client
}

// NewClient creates a new HTTP client with custom transport settings.
func NewClient() *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultConnectTimeout,
			KeepAlive: keepAlivePeriod,
		}).DialContext,
		MaxIdleConns:          maxIdleConns,
		IdleConnTimeout:       defaultIdleTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		DisableCompression:    true,
		MaxConnsPerHost:       maxConnsPerHost,
	}

	return &Client{
		&http.Client{
			Transport: transport,
		},
	}
}

// IsDownloadable checks if the given URL is downloadable.
func IsDownloadable(urlStr string) bool {
	_, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	resp, err := http.Head(urlStr)
	if err != nil || resp.StatusCode >= 400 {
		// Fallback to GET if HEAD fails
		resp, err = http.Get(urlStr)
		if err != nil {
			return false
		}
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close response body", "err", err)
		}
	}()

	finalURL := resp.Request.URL

	return (finalURL.Scheme == "http" || finalURL.Scheme == "https") && isDownloadableContent(resp)
}

// isDownloadableContent checks if the response represents downloadable content.
func isDownloadableContent(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	contentDisp := resp.Header.Get("Content-Disposition")

	// Explicit attachment disposition means downloadable
	if contentDisp != "" {
		return true
	}

	// Check content type
	if contentType != "" {
		// HTML is typically not downloadable (it's a web page)
		if strings.HasPrefix(contentType, "text/html") {
			return false
		}

		return true
	}

	return true
}

// Head performs a HEAD request to the specified URL with optional headers.
func (c *Client) Head(ctx context.Context, urlStr string, headers map[string]string) (*http.Response, error) {
	slog.Debug("initializing with HEAD request", "url", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodHead, headers)
	if err != nil {
		slog.Error("failed to create HEAD request", "url", urlStr, "err", err)
		return nil, err
	}

	slog.Debug("sending HEAD request", "url", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		slog.Error("HEAD request failed", "url", urlStr, "err", err)
		return nil, ClassifyError(err)
	}

	slog.Debug("HEAD response", "url", urlStr, "status", resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("HEAD request returned error status", "status", resp.StatusCode, "url", urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return resp, nil
}

// Range performs a Range GET request to the specified URL. It takes in the start byte and the end byte.
func (c *Client) Range(ctx context.Context, urlStr string, start, end int64, headers map[string]string) (*http.Response, error) {
	slog.Debug("initializing with Range GET request", "url", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodGet, headers)
	if err != nil {
		slog.Error("failed to create Range GET request", "url", urlStr, "err", err)
		return nil, err
	}

	rangeVal := fmt.Sprintf("bytes=%d-%d", start, end)
	req.Header.Set("Range", rangeVal)
	slog.Debug("set Range header", "range", rangeVal, "url", urlStr)

	slog.Debug("sending Range GET request", "url", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		slog.Error("Range GET request failed", "url", urlStr, "err", err)
		return nil, ClassifyError(err)
	}

	slog.Debug("Range GET response", "url", urlStr, "status", resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("Range GET request returned error status", "status", resp.StatusCode, "url", urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	if resp.StatusCode != http.StatusPartialContent {
		slog.Warn("server doesn't support ranges", "url", urlStr, "status", resp.StatusCode)
		return nil, ErrRangesNotSupported
	}

	return resp, nil
}

// Get performs a GET request to the specified URL.
func (c *Client) Get(ctx context.Context, urlStr string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		slog.Error("failed to create fallback GET request", "err", err)
		return nil, ErrRequestCreation
	}

	slog.Debug("sending fallback GET request", "url", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		slog.Error("fallback GET request failed", "err", err)
		return nil, ClassifyError(err)
	}

	slog.Debug("closing body immediately for fallback GET request")

	slog.Debug("GET response", "url", urlStr, "status", resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("GET request returned error status", "status", resp.StatusCode, "url", urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return resp, nil
}

// generateRequest creates a new HTTP request with the specified method and URL.
func generateRequest(ctx context.Context, urlStr, method string, headers map[string]string) (*http.Request, error) {
	slog.Debug("creating request", "method", method, "url", urlStr)

	req, err := http.NewRequestWithContext(ctx, method, urlStr, http.NoBody)
	if err != nil {
		slog.Error("failed to create request", "method", method, "url", urlStr, "err", err)
		return nil, ErrRequestCreation
	}

	req.Header.Set("User-Agent", DefaultUserAgent)

	for key, value := range headers {
		req.Header.Set(key, value)
		slog.Debug("set custom header", "key", key)
	}

	return req, nil
}

// GetFilename tries extracts the filename from the Content-Disposition header or the URL.
func GetFilename(resp *http.Response) string {
	fileName, ok := getFileNameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	if ok {
		return fileName
	}

	u := resp.Request.URL
	if qname := u.Query().Get("filename"); qname != "" {
		return qname
	}

	base := path.Base(u.Path)
	if base != "" && base != "/" {
		return base
	}

	return defaultDownloadName
}

func getFileNameFromContentDisposition(header string) (string, bool) {
	if header == "" {
		return "", false
	}

	if _, params, err := mime.ParseMediaType(header); err == nil {
		if fName := cmp.Or(params["filename"], params["filename*"]); fName != "" {
			return fName, true
		}
	}

	return "", false
}

// ParseLastModified parses the Last-Modified header.
func ParseLastModified(header string) time.Time {
	if header == "" {
		return time.Time{}
	}

	// Try to parse the header (RFC1123 format)
	t, err := time.Parse(time.RFC1123, header)
	if err != nil {
		slog.Debug("failed to parse Last-Modified header", "header", header, "err", err)
		return time.Time{}
	}

	slog.Debug("parsed Last-Modified", "time", t)

	return t
}
