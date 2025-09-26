package http

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/NamanBalaji/tdm/internal/logger"
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
			logger.Warnf("Failed to close response body: %v", err)
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
	logger.Debugf("Initializing with HEAD request: %s", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodHead, headers)
	if err != nil {
		logger.Errorf("Failed to create HEAD request for %s: %v", urlStr, err)
		return nil, err
	}

	logger.Debugf("Sending HEAD request to %s", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		logger.Errorf("HEAD request failed for %s: %v", urlStr, err)
		return nil, ClassifyError(err)
	}

	logger.Debugf("HEAD response for %s: status=%d", urlStr, resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		logger.Errorf("HEAD request returned error status %d for %s", resp.StatusCode, urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return resp, nil
}

// Range performs a Range GET request to the specified URL. It takes in the start byte and the end byte.
func (c *Client) Range(ctx context.Context, urlStr string, start, end int64, headers map[string]string) (*http.Response, error) {
	logger.Debugf("Initializing with Range GET request: %s", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodGet, headers)
	if err != nil {
		logger.Errorf("Failed to create Range GET request for %s: %v", urlStr, err)
		return nil, err
	}

	rangeVal := fmt.Sprintf("bytes=%d-%d", start, end)
	req.Header.Set("Range", rangeVal)
	logger.Debugf("Set Range header: bytes=0-0 for %s", urlStr)

	logger.Debugf("Sending Range GET request to %s", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		logger.Errorf("Range GET request failed for %s: %v", urlStr, err)
		return nil, ClassifyError(err)
	}

	logger.Debugf("Range GET response for %s: status=%d", urlStr, resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		logger.Errorf("Range GET request returned error status %d for %s", resp.StatusCode, urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	if resp.StatusCode != http.StatusPartialContent {
		logger.Warnf("Server doesn't support ranges for %s (status: %d)", urlStr, resp.StatusCode)
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
		logger.Errorf("Failed to create fallback GET request: %v", err)
		return nil, ErrRequestCreation
	}

	logger.Debugf("Sending fallback GET request to %s", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		logger.Errorf("Fallback GET request failed: %v", err)
		return nil, ClassifyError(err)
	}

	logger.Debugf("Closing body immediately for fallback GET request")

	logger.Debugf("GET response for %s: status=%d", urlStr, resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		logger.Errorf("GET request returned error status %d for %s", resp.StatusCode, urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return resp, nil
}

// generateRequest creates a new HTTP request with the specified method and URL.
func generateRequest(ctx context.Context, urlStr, method string, headers map[string]string) (*http.Request, error) {
	logger.Debugf("Creating %s request for URL: %s", method, urlStr)

	req, err := http.NewRequestWithContext(ctx, method, urlStr, http.NoBody)
	if err != nil {
		logger.Errorf("Failed to create %s request for %s: %v", method, urlStr, err)
		return nil, ErrRequestCreation
	}

	req.Header.Set("User-Agent", DefaultUserAgent)

	for key, value := range headers {
		req.Header.Set(key, value)
		logger.Debugf("Set custom header: %s", key)
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
		if fName, ok := params["filename"]; ok {
			return fName, true
		}

		if fName, ok := params["filename*"]; ok {
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
		logger.Debugf("Failed to parse Last-Modified header: %s, error: %v", header, err)
		return time.Time{}
	}

	logger.Debugf("Parsed Last-Modified: %v", t)

	return t
}
