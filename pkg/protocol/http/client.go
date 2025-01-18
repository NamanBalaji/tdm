package http

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range c.config.DefaultHeaders {
		req.Header.Set(k, v)
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HEAD request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	filename := c.getFilename(resp.Header, parsedURL)

	return &protocol.FileInfo{
		Size:         resp.ContentLength,
		Resumable:    strings.Contains(resp.Header.Get("Accept-Ranges"), "bytes"),
		Filename:     filename,
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: resp.Header.Get("Last-Modified"),
		ETag:         resp.Header.Get("ETag"),
	}, nil
}

func (c *HTTPClient) getFilename(header http.Header, parsedURL *url.URL) string {
	if cd := header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename := params["filename"]; filename != "" {
				return filename
			}
		}
	}

	if path := parsedURL.Path; path != "" {
		if segments := strings.Split(path, "/"); len(segments) > 0 {
			if filename := segments[len(segments)-1]; filename != "" {
				return filename
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
