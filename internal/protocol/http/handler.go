package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NamanBalaji/tdm/internal/common"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultReadTimeout    = 30 * time.Second
	defaultIdleTimeout    = 90 * time.Second
	defaultUserAgent      = "TDM/1.0"

	defaultDownloadName = "download"
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

func (h *Handler) Initialize(urlStr string, config *downloader.Config) (*common.DownloadInfo, error) {
	info, err := h.initializeWithHEAD(urlStr, config)
	if err == nil {
		return info, nil
	}

	if IsFallbackError(err) {
		info, err = h.initializeWithRangeGET(urlStr, config)
		if err == nil {
			return info, nil
		}
	}

	if IsFallbackError(err) {
		return h.initializeWithRegularGET(urlStr, config)
	}

	return nil, err
}

func (h *Handler) CreateConnection(urlString string, chunk *chunk.Chunk, downloadConfig *downloader.Config) (connection.Connection, error) {
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

	// Apply headers from the download config
	if downloadConfig.Headers != nil {
		for key, value := range downloadConfig.Headers {
			conn.headers[key] = value
		}
	}

	return conn, nil
}

// initializeWithHEAD attempts to initialize using a HEAD request
func (h *Handler) initializeWithHEAD(urlStr string, config *downloader.Config) (*common.DownloadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodHead, config)
	if err != nil {
		return nil, err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, ClassifyError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, ErrHeadNotSupported
	}

	return generateInfo(urlStr, resp, resp.Header.Get("Accept-Ranges") == "bytes", resp.ContentLength), nil
}

// initializeWithRangeGET tries to get file info using Range headers
func (h *Handler) initializeWithRangeGET(urlStr string, config *downloader.Config) (*common.DownloadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodGet, config)
	if err != nil {
		return nil, err
	}

	// Set range header to get just the first byte
	req.Header.Set("Range", "bytes=0-0")

	// Execute the GET request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, ClassifyError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	// Check if range request was rejected or returned an error
	if resp.StatusCode != 206 {
		return nil, ErrRangesNotSupported
	}

	// Try to parse Content-Range header to get total size
	contentRange := resp.Header.Get("Content-Range")
	var totalSize int64 = -1
	if contentRange != "" {
		// Format: bytes 0-0/1234
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			size, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				totalSize = size
			}
		}
	}

	return generateInfo(urlStr, resp, true, totalSize), nil
}

// initializeWithRegularGET gets file info using a regular GET request
// This is the final fallback when both HEAD and Range requests fail
func (h *Handler) initializeWithRegularGET(urlStr string, config *downloader.Config) (*common.DownloadInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodGet, config)
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
		tempReq, _ := http.NewRequestWithContext(ctx, "GET", urlStr, http.NoBody)
		for k, v := range req.Header {
			tempReq.Header[k] = v
		}

		resp, err = h.client.Do(tempReq)
		if err != nil {
			return nil, ClassifyError(err)
		}
		// We'll close the body immediately after getting headers
		resp.Body.Close()
	} else if resp.Body != nil {
		defer resp.Body.Close()
	}

	// Check if the server returned an error status
	if resp.StatusCode >= 400 {
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return generateInfo(urlStr, resp, false, resp.ContentLength), nil
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
		return defaultDownloadName
	}

	path := u.Path
	segments := strings.Split(path, "/")
	if len(segments) > 0 {
		filename := segments[len(segments)-1]
		if filename != "" {
			return filename
		}
	}

	return defaultDownloadName
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

func generateRequest(ctx context.Context, urlStr, method string, config *downloader.Config) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", defaultUserAgent)

	if config != nil && config.Headers != nil {
		for key, value := range config.Headers {
			req.Header.Set(key, value)
		}
	}

	return req, nil
}

func generateInfo(urlStr string, resp *http.Response, canRange bool, totalSize int64) *common.DownloadInfo {
	info := &common.DownloadInfo{
		URL:             urlStr,
		MimeType:        resp.Header.Get("Content-Type"),
		TotalSize:       totalSize,
		SupportsRanges:  canRange,
		LastModified:    parseLastModified(resp.Header.Get("Last-Modified")),
		ETag:            resp.Header.Get("ETag"),
		AcceptRanges:    canRange,
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Server:          resp.Header.Get("Server"),
		CanBeResumed:    canRange,
	}

	filename := parseContentDisposition(resp.Header.Get("Content-Disposition"))
	if filename != "" {
		info.Filename = filename
	} else {
		// Extract filename from URL if not found in headers
		info.Filename = extractFilenameFromURL(info.URL)
	}

	return info
}
