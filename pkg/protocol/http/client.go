package http

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NamanBalaji/tdm/pkg/protocol"
)

type HTTPClient struct {
	client    *http.Client
	transport *http.Transport
	config    ClientConfig
}

func NewClient(config *ClientConfig) protocol.Protocol {
	if config == nil {
		config = DefaultConfig()
	}

	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     config.MaxConnsPerHost,
		IdleConnTimeout:     config.IdleConnTimeout,
		TLSHandshakeTimeout: config.TLSHandshakeTimeout,

		DialContext: (&net.Dialer{
			Timeout:   config.DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	if config.ProxyURL != nil {
		transport.Proxy = http.ProxyURL(config.ProxyURL)
	}

	if config.TLSConfig != nil {
		transport.TLSClientConfig = config.TLSConfig
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= config.MaxRedirects {
				return &HTTPError{
					Type:      ErrorTypeHTTP,
					Operation: "redirect",
					URL:       req.URL.String(),
					Status:    http.StatusTooManyRequests,
					Err:       fmt.Errorf("too many redirects (max: %d)", config.MaxRedirects),
				}
			}
			return nil
		},
	}

	return &HTTPClient{
		client:    client,
		transport: transport,
		config:    *config,
	}
}

func (c *HTTPClient) Initialize(ctx context.Context, urlStr string, opts protocol.DownloadOptions) (*protocol.FileInfo, error) {
	if len(urlStr) == 0 {
		return nil, fmt.Errorf("url is empty")
	}

	if !c.Supports(urlStr) {
		return nil, fmt.Errorf("url does not support HTTP/HTTPS")
	}

	info, headErr := c.headRequest(ctx, urlStr, opts)
	if headErr == nil {
		return info, nil
	}

	var httpErr *HTTPError
	if isHttpError := errors.As(headErr, &httpErr); !isHttpError {
		return nil, headErr
	}

	if httpErr.Status != http.StatusMethodNotAllowed && httpErr.Status != http.StatusForbidden {
		return nil, headErr
	}

	fallbackInfo, fbErr := c.fallbackRangeCheck(ctx, urlStr, opts)
	if fbErr != nil {
		return nil, fmt.Errorf("HEAD error: %w, fallback GET error: %v", headErr, fbErr)
	}

	return fallbackInfo, nil
}

func (c *HTTPClient) headRequest(ctx context.Context, urlStr string, opts protocol.DownloadOptions) (*protocol.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HEAD request: %w", err)
	}

	c.applyHeaders(req, opts)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, NewHTTPNetworkError("HEAD", urlStr, err)
	}
	defer resp.Body.Close()

	// Some servers may return 405 (Method Not Allowed) for HEAD, or 403, or etc.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, NewHTTPStatusError("HEAD", urlStr, resp.StatusCode,
			fmt.Errorf("HEAD request returned status %d", resp.StatusCode))
	}

	filename := c.getFilename(resp.Header, urlStr)
	return &protocol.FileInfo{
		Size:         resp.ContentLength,
		Resumable:    strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes"),
		Filename:     filename,
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: resp.Header.Get("Last-Modified"),
		ETag:         resp.Header.Get("ETag"),
	}, nil
}

func (c *HTTPClient) fallbackRangeCheck(ctx context.Context, urlStr string, opts protocol.DownloadOptions) (*protocol.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create fallback GET request: %w", err)
	}

	c.applyHeaders(req, opts)
	req.Header.Set("Range", "bytes=0-0") // minimal range request

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, NewHTTPNetworkError("fallbackGET", urlStr, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent: // 206
		// Server supports ranges
		totalSize := int64(-1)
		if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
			// Parse "bytes 0-0/1234" format
			if parts := strings.Split(contentRange, "/"); len(parts) == 2 {
				if size, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					totalSize = size
				}
			}
		}

		return &protocol.FileInfo{
			Size:         totalSize,
			Resumable:    true,
			Filename:     c.getFilename(resp.Header, urlStr),
			ContentType:  resp.Header.Get("Content-Type"),
			LastModified: resp.Header.Get("Last-Modified"),
			ETag:         resp.Header.Get("ETag"),
		}, nil

	case http.StatusOK: // 200
		resp.Body.Close()

		return &protocol.FileInfo{
			Size:         resp.ContentLength,
			Resumable:    false,
			Filename:     c.getFilename(resp.Header, urlStr),
			ContentType:  resp.Header.Get("Content-Type"),
			LastModified: resp.Header.Get("Last-Modified"),
			ETag:         resp.Header.Get("ETag"),
		}, nil

	default:
		return nil, NewHTTPStatusError("GET", urlStr, resp.StatusCode,
			fmt.Errorf("unexpected status code"))
	}
}

func (c *HTTPClient) applyHeaders(req *http.Request, opts protocol.DownloadOptions) {
	for k, v := range c.config.DefaultHeaders {
		req.Header.Set(k, v)
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
}

func (c *HTTPClient) getFilename(header http.Header, urlStr string) string {
	if cd := header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename := params["filename"]; filename != "" {
				return filename
			}
		}
	}

	parsedURL, _ := url.Parse(urlStr)
	if parsedURL != nil && parsedURL.Path != "" {
		segments := strings.Split(parsedURL.Path, "/")
		if len(segments) > 0 {
			last := segments[len(segments)-1]
			if last != "" {
				return last
			}
		}
	}

	return "download"
}

func (c *HTTPClient) CreateDownloader(ctx context.Context, url string) (protocol.Downloader, error) {
	return nil, errors.New("not implemented")
}

func (c *HTTPClient) Supports(urlStr string) bool {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

func (c *HTTPClient) Cleanup() error {
	c.transport.CloseIdleConnections()
	return nil
}
