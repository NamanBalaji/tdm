package http

import (
	"context"
	"fmt"
	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultReadTimeout    = 30 * time.Second
	defaultWriteTimeout   = 30 * time.Second
	defaultIdleTimeout    = 90 * time.Second
	defaultUserAgent      = "TDM/1.0"
)

// Handler implements the Protocol interface for HTTP/HTTPS
type Handler struct {
	client *http.Client
}

// NewHandler creates a new HTTP protocol handler
func NewHandler() *Handler {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       defaultIdleTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true, // We want raw data, not compressed
		MaxConnsPerHost:       16,   // Default to aria2-like concurrent connections
	}

	// Create the client with our custom transport
	client := &http.Client{
		Transport: transport,
		Timeout:   defaultReadTimeout,
	}

	return &Handler{
		client: client,
	}
}

func (h *Handler) CanHandle(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (h *Handler) Initialize(urlStr string, options *downloader.DownloadOptions) (*downloader.DownloadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	// Create a HEAD request to get file information
	req, err := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add common headers
	req.Header.Set("User-Agent", defaultUserAgent)

	// Add custom headers if provided
	if options != nil && options.Headers != nil {
		for key, value := range options.Headers {
			req.Header.Set(key, value)
		}
	}
	resp, err := h.client.Do(req)
	if err != nil {
		// If HEAD fails, try GET with Range: bytes=0-0
		return h.initializeWithGET(urlStr, options)
	}
	defer resp.Body.Close()

	// Check if the server returned an error status
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned error status: %s", resp.Status)
	}

	// Extract file information
	info := &downloader.DownloadInfo{
		URL:             urlStr,
		MimeType:        resp.Header.Get("Content-Type"),
		TotalSize:       resp.ContentLength,
		SupportsRanges:  resp.Header.Get("Accept-Ranges") == "bytes",
		LastModified:    parseLastModified(resp.Header.Get("Last-Modified")),
		ETag:            resp.Header.Get("ETag"),
		AcceptRanges:    resp.Header.Get("Accept-Ranges") == "bytes",
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Server:          resp.Header.Get("Server"),
		CanBeResumed:    resp.Header.Get("Accept-Ranges") == "bytes",
	}

	// Try to get filename from Content-Disposition header
	filename := parseContentDisposition(resp.Header.Get("Content-Disposition"))
	if filename != "" {
		info.Filename = filename
	} else {
		// Extract filename from URL if not found in headers
		info.Filename = extractFilenameFromURL(urlStr)
	}

	return info, nil
}

func (h *Handler) CreateConnection(urlString string, chunk *chunk.Chunk, options *downloader.DownloadOptions) (connection.Connection, error) {
	conn := &Connection{
		url:       urlString,
		headers:   make(map[string]string),
		client:    h.client,
		startByte: chunk.StartByte + chunk.Downloaded, // Resume from where we left off
		endByte:   chunk.EndByte,
		timeout:   defaultReadTimeout,
	}

	conn.headers["User-Agent"] = defaultUserAgent

	if chunk.StartByte > 0 || chunk.EndByte < chunk.Size()-1 {
		conn.headers["Range"] = fmt.Sprintf("bytes=%d-%d", conn.startByte, chunk.EndByte)
	}

	if options.Headers != nil {
		for key, value := range options.Headers {
			conn.headers[key] = value
		}
	}

	return conn, nil
}

// initializeWithGET attempts to initialize using a GET request with Range header
// This is a fallback when HEAD requests are not supported by the server
func (h *Handler) initializeWithGET(urlStr string, options *downloader.DownloadOptions) (*downloader.DownloadInfo, error) {
	// First try with a range request to test if server supports ranges
	info, rangeSupported, err := h.initializeWithRangeGET(urlStr, options)
	if err == nil {
		return info, nil
	}

	// If range request failed, try with regular GET (but only get headers)
	if !rangeSupported {
		return h.initializeWithRegularGET(urlStr, options)
	}

	return nil, err
}

// initializeWithRangeGET tries to get file info using Range headers
func (h *Handler) initializeWithRangeGET(urlStr string, options *downloader.DownloadOptions) (*downloader.DownloadInfo, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	req, err := generateGETRequest(ctx, urlStr, options)
	if err != nil {
		return nil, false, err
	}

	// Set range header to get just the first byte
	req.Header.Set("Range", "bytes=0-0")

	// Execute the GET request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	// Check if range request was rejected or returned an error
	if resp.StatusCode != 206 {
		return nil, false, fmt.Errorf("server does not support range requests")
	}

	// Extract file information
	info := &downloader.DownloadInfo{
		URL:             urlStr,
		MimeType:        resp.Header.Get("Content-Type"),
		TotalSize:       -1,   // Unknown size
		SupportsRanges:  true, // Confirmed by 206 status
		LastModified:    parseLastModified(resp.Header.Get("Last-Modified")),
		ETag:            resp.Header.Get("ETag"),
		AcceptRanges:    true,
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Server:          resp.Header.Get("Server"),
		CanBeResumed:    true,
	}

	// Try to parse Content-Range header to get total size
	contentRange := resp.Header.Get("Content-Range")
	if contentRange != "" {
		// Format: bytes 0-0/1234
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			size, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				info.TotalSize = size
			}
		}
	}

	// Try to get filename from Content-Disposition header
	filename := parseContentDisposition(resp.Header.Get("Content-Disposition"))
	if filename != "" {
		info.Filename = filename
	} else {
		info.Filename = extractFilenameFromURL(urlStr)
	}

	return info, true, nil
}

// initializeWithRegularGET gets file info using a regular GET request
// This is the final fallback when both HEAD and Range requests fail
func (h *Handler) initializeWithRegularGET(urlStr string, options *downloader.DownloadOptions) (*downloader.DownloadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	req, err := generateGETRequest(ctx, urlStr, options)
	if err != nil {
		return nil, err
	}

	// Execute the GET request
	// Add a "Get-Only-Header" header which our client will detect to abort
	// the download after headers are received (technique to avoid downloading
	// the entire file just to get metadata)
	req.Header.Set("X-TDM-Get-Only-Headers", "true")

	// Create a custom transport to intercept the response
	transport := http.DefaultTransport.(*http.Transport).Clone()
	originalDialContext := transport.DialContext

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := originalDialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		// Wrap the connection to close it after reading headers
		return &headerOnlyConn{Conn: conn}, nil
	}

	// Create a client with the custom transport
	client := &http.Client{
		Transport: transport,
		Timeout:   defaultConnectTimeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		// If we get "unexpected EOF" or similar, it's expected due to our connection closing
		// Instead, try again with a normal client but we'll abort the body read
		tempReq, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		for k, v := range req.Header {
			tempReq.Header[k] = v
		}

		resp, err = h.client.Do(tempReq)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to server: %w", err)
		}
		resp.Body.Close()
	} else if resp.Body != nil {
		defer resp.Body.Close()
	}

	// Check if the server returned an error status
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned error status: %s", resp.Status)
	}

	// Extract file information
	info := &downloader.DownloadInfo{
		URL:             urlStr,
		MimeType:        resp.Header.Get("Content-Type"),
		TotalSize:       resp.ContentLength, // May be -1 if unknown
		SupportsRanges:  false,              // We already determined ranges aren't supported
		LastModified:    parseLastModified(resp.Header.Get("Last-Modified")),
		ETag:            resp.Header.Get("ETag"),
		AcceptRanges:    false,
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Server:          resp.Header.Get("Server"),
		CanBeResumed:    false, // Cannot resume without ranges
	}

	// Try to get filename from Content-Disposition header
	filename := parseContentDisposition(resp.Header.Get("Content-Disposition"))
	if filename != "" {
		info.Filename = filename
	} else {
		// Extract filename from URL if not found in headers
		info.Filename = extractFilenameFromURL(urlStr)
	}

	return info, nil
}

// headerOnlyConn is a net.Conn wrapper that closes after reading headers
type headerOnlyConn struct {
	net.Conn
	readHeaders bool
}

func (c *headerOnlyConn) Read(b []byte) (int, error) {
	if c.readHeaders {
		return 0, io.EOF
	}

	n, err := c.Conn.Read(b)

	// Check if we've read the headers (look for double CRLF)
	if n > 3 {
		for i := 0; i < n-3; i++ {
			if b[i] == '\r' && b[i+1] == '\n' && b[i+2] == '\r' && b[i+3] == '\n' {
				c.readHeaders = true
				return i + 4, io.EOF
			}
		}
	}

	return n, err
}

// parseContentDisposition extracts filename from Content-Disposition header
func parseContentDisposition(header string) string {
	if header == "" {
		return ""
	}

	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "filename=") {
			// Extract the filename
			filename := strings.TrimPrefix(part, "filename=")
			filename = strings.TrimPrefix(filename, "\"")
			filename = strings.TrimSuffix(filename, "\"")
			return filename
		}
	}

	return ""
}

// extractFilenameFromURL extracts the filename from a URL
func extractFilenameFromURL(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "download"
	}

	path := u.Path
	segments := strings.Split(path, "/")
	if len(segments) > 0 {
		filename := segments[len(segments)-1]
		if filename != "" {
			return filename
		}
	}

	return "download"
}

// parseLastModified parses the Last-Modified header
func parseLastModified(header string) time.Time {
	if header == "" {
		return time.Time{}
	}

	// Try to parse the header (RFC1123 format)
	t, err := time.Parse(time.RFC1123, header)
	if err != nil {
		return time.Time{}
	}

	return t
}

func generateGETRequest(ctx context.Context, urlStr string, options *downloader.DownloadOptions) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set common headers
	req.Header.Set("User-Agent", defaultUserAgent)

	// Add custom headers if provided
	if options != nil && options.Headers != nil {
		for key, value := range options.Headers {
			req.Header.Set(key, value)
		}
	}

	return req, nil
}
