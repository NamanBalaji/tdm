package http

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/logger"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultReadTimeout    = 30 * time.Second
	defaultIdleTimeout    = 90 * time.Second
	defaultUserAgent      = "TDM/1.0"

	defaultDownloadName = "download"
)

// Handler implements the Protocol interface for HTTP/HTTPS.
type Handler struct {
	client *http.Client
}

// NewHandler creates a new HTTP protocol handler.
func NewHandler() *Handler {
	logger.Debugf("Creating new HTTP handler")

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
		DisableCompression:    true,
		MaxConnsPerHost:       16,
	}

	client := &http.Client{
		Transport: transport,
	}

	logger.Debugf("HTTP handler created with timeout settings: connect=%v, read=%v, idle=%v",
		defaultConnectTimeout, defaultReadTimeout, defaultIdleTimeout)

	return &Handler{
		client: client,
	}
}

func (h *Handler) CanHandle(urlStr string) bool {
	_, err := url.Parse(urlStr)
	if err != nil {
		logger.Debugf("Cannot parse URL: %s, error: %v", urlStr, err)
		return false
	}

	resp, err := http.Head(urlStr)
	if err != nil || resp.StatusCode >= 400 {
		logger.Errorf("HEAD request failed or unsupported (%v), falling back to GET...", err)
		resp, err = http.Get(urlStr)
		if err != nil {
			logger.Errorf("GET request also failed: %v", err)
			return false
		}
	}
	finalURL := resp.Request.URL
	resp.Body.Close()

	return (finalURL.Scheme == "http" || finalURL.Scheme == "https") && checkDownloadable(resp)
}

// checkDownloadable checks if the response is downloadable.
func checkDownloadable(res *http.Response) bool {
	contentType := res.Header.Get("Content-Type")
	contentDisp := res.Header.Get("Content-Disposition")

	if contentDisp != "" {
		return true
	} else if contentType != "" {
		if strings.HasPrefix(contentType, "text/html") {
			return false
		} else {
			return true
		}
	}
	return true
}

func (h *Handler) Initialize(ctx context.Context, urlStr string, config *common.Config) (*common.DownloadInfo, error) {
	logger.Debugf("Initializing download for URL: %s", urlStr)

	logger.Debugf("Attempting HEAD request for %s", urlStr)
	info, err := h.initializeWithHEAD(ctx, urlStr, config)
	if err == nil {
		logger.Debugf("HEAD request successful for %s", urlStr)
		return info, nil
	}
	logger.Debugf("HEAD request failed for %s: %v", urlStr, err)

	if IsFallbackError(err) {
		logger.Debugf("Falling back to Range GET request for %s", urlStr)
		info, err = h.initializeWithRangeGET(ctx, urlStr, config)
		if err == nil {
			logger.Debugf("Range GET request successful for %s", urlStr)
			return info, nil
		}
		logger.Debugf("Range GET request failed for %s: %v", urlStr, err)
	}

	if IsFallbackError(err) {
		logger.Debugf("Falling back to regular GET request for %s", urlStr)
		info, err := h.initializeWithRegularGET(ctx, urlStr)
		if err == nil {
			logger.Debugf("Regular GET request successful for %s", urlStr)
			return info, nil
		}
		logger.Debugf("Regular GET request failed for %s: %v", urlStr, err)
	}

	logger.Errorf("All request methods failed for %s", urlStr)
	return nil, err
}

func (h *Handler) CreateConnection(urlString string, chunk *chunk.Chunk, downloadConfig *common.Config) (connection.Connection, error) {
	logger.Debugf("Creating HTTP connection for chunk %s (bytes %d-%d, downloaded: %d)",
		chunk.ID, chunk.GetStartByte(), chunk.GetEndByte(), chunk.GetDownloaded())

	// Get the current byte range
	currentStart, endByte := chunk.GetCurrentByteRange()

	headers := make(map[string]string)
	headers["User-Agent"] = defaultUserAgent

	// Add custom headers if provided
	if downloadConfig != nil && downloadConfig.Headers != nil {
		for key, value := range downloadConfig.Headers {
			headers[key] = value
			logger.Debugf("Added custom header: %s for chunk %s", key, chunk.ID)
		}
	}

	headers["Range"] = fmt.Sprintf("bytes=%d-%d", currentStart, chunk.EndByte)

	conn := NewConnection(urlString, headers, h.client, currentStart, endByte)

	return conn, nil
}

func (h *Handler) UpdateConnection(conn connection.Connection, chunk *chunk.Chunk) {
	logger.Debugf("Updating connection for chunk %s (bytes %d-%d)", chunk.ID, chunk.StartByte, chunk.EndByte)

	currentStart, endByte := chunk.GetCurrentByteRange()

	rangeHeader := fmt.Sprintf("bytes=%d-%d", currentStart, endByte)
	conn.SetHeader("Range", rangeHeader)

	logger.Debugf("Updated Range header for chunk %s: %s", chunk.ID, rangeHeader)
}

// initializeWithHEAD attempts to initialize using a HEAD request.
func (h *Handler) initializeWithHEAD(ctx context.Context, urlStr string, config *common.Config) (*common.DownloadInfo, error) {
	logger.Debugf("Initializing with HEAD request: %s", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodHead, config)
	if err != nil {
		logger.Errorf("Failed to create HEAD request for %s: %v", urlStr, err)
		return nil, err
	}

	logger.Debugf("Sending HEAD request to %s", urlStr)
	resp, err := h.client.Do(req)
	if err != nil {
		logger.Errorf("HEAD request failed for %s: %v", urlStr, err)
		return nil, classifyError(err)
	}
	defer resp.Body.Close()

	logger.Debugf("HEAD response for %s: status=%d", urlStr, resp.StatusCode)
	if resp.StatusCode >= 400 {
		logger.Errorf("HEAD request returned error status %d for %s", resp.StatusCode, urlStr)
		return nil, classifyHTTPError(resp.StatusCode)
	}

	supportsRanges := resp.Header.Get("Accept-Ranges") == "bytes"
	logger.Debugf("HEAD request successful, content-length=%d, supports-ranges=%v",
		resp.ContentLength, supportsRanges)

	return generateInfo(resp, supportsRanges, resp.ContentLength), nil
}

// initializeWithRangeGET tries to get file info using Range headers.
func (h *Handler) initializeWithRangeGET(ctx context.Context, urlStr string, config *common.Config) (*common.DownloadInfo, error) {
	logger.Debugf("Initializing with Range GET request: %s", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodGet, config)
	if err != nil {
		logger.Errorf("Failed to create Range GET request for %s: %v", urlStr, err)
		return nil, err
	}

	req.Header.Set("Range", "bytes=0-0")
	logger.Debugf("Set Range header: bytes=0-0 for %s", urlStr)

	logger.Debugf("Sending Range GET request to %s", urlStr)
	resp, err := h.client.Do(req)
	if err != nil {
		logger.Errorf("Range GET request failed for %s: %v", urlStr, err)
		return nil, classifyError(err)
	}
	defer resp.Body.Close()

	logger.Debugf("Range GET response for %s: status=%d", urlStr, resp.StatusCode)
	if resp.StatusCode >= 400 {
		logger.Errorf("Range GET request returned error status %d for %s", resp.StatusCode, urlStr)
		return nil, classifyHTTPError(resp.StatusCode)
	}

	if resp.StatusCode != http.StatusPartialContent {
		logger.Warnf("Server doesn't support ranges for %s (status: %d)", urlStr, resp.StatusCode)
		return nil, ErrRangesNotSupported
	}

	contentRange := resp.Header.Get("Content-Range")
	var totalSize int64 = 0
	if contentRange != "" {
		// Format: bytes 0-0/1234
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			size, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				logger.Warnf("Failed to parse size from Content-Range header: %s", contentRange)
				return nil, ErrInvalidContentRange
			}
			totalSize = size
			logger.Debugf("Parsed total size from Content-Range: %d bytes", totalSize)
		}
	}

	logger.Debugf("Range GET request successful, supports-ranges=true, content-length=%d", totalSize)
	return generateInfo(resp, true, totalSize), nil
}

// This is the final fallback when both HEAD and Range requests fail.
func (h *Handler) initializeWithRegularGET(ctx context.Context, urlStr string) (*common.DownloadInfo, error) {
	logger.Debugf("Initializing with regular GET request: %s", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		logger.Errorf("Failed to create fallback GET request: %v", err)
		return nil, ErrRequestCreation
	}

	logger.Debugf("Sending fallback GET request to %s", urlStr)
	resp, err := h.client.Do(req)
	if err != nil {
		logger.Errorf("Fallback GET request failed: %v", err)
		return nil, classifyError(err)
	}
	logger.Debugf("Closing body immediately for fallback GET request")
	resp.Body.Close()

	logger.Debugf("GET response for %s: status=%d", urlStr, resp.StatusCode)
	if resp.StatusCode >= 400 {
		logger.Errorf("GET request returned error status %d for %s", resp.StatusCode, urlStr)
		return nil, classifyHTTPError(resp.StatusCode)
	}

	logger.Debugf("Regular GET request successful, content-length=%d", resp.ContentLength)
	return generateInfo(resp, false, resp.ContentLength), nil
}

// generateRequest creates a new HTTP request with the specified method and URL.
func generateRequest(ctx context.Context, urlStr, method string, config *common.Config) (*http.Request, error) {
	logger.Debugf("Creating %s request for URL: %s", method, urlStr)

	req, err := http.NewRequestWithContext(ctx, method, urlStr, http.NoBody)
	if err != nil {
		logger.Errorf("Failed to create %s request for %s: %v", method, urlStr, err)
		return nil, ErrRequestCreation
	}

	req.Header.Set("User-Agent", defaultUserAgent)
	logger.Debugf("Set User-Agent: %s", defaultUserAgent)

	if config != nil && config.Headers != nil {
		for key, value := range config.Headers {
			req.Header.Set(key, value)
			logger.Debugf("Set custom header: %s", key)
		}
	}

	return req, nil
}

// generateInfo generates download info from the response.
func generateInfo(resp *http.Response, canRange bool, totalSize int64) *common.DownloadInfo {
	logger.Debugf("Generating download info for %s", resp.Request.URL)

	info := &common.DownloadInfo{
		URL:             resp.Request.URL.String(),
		MimeType:        resp.Header.Get("Content-Type"),
		TotalSize:       totalSize,
		SupportsRanges:  canRange,
		LastModified:    parseLastModified(resp.Header.Get("Last-Modified")),
		ETag:            resp.Header.Get("ETag"),
		AcceptRanges:    canRange,
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Server:          resp.Header.Get("Server"),
		CanBeResumed:    canRange,
		Filename:        getFilename(resp),
	}

	logger.Debugf("Download info generated: filename=%s, size=%d, supports-ranges=%v, type=%s",
		info.Filename, info.TotalSize, info.SupportsRanges, info.MimeType)

	return info
}

// getFilename tries extracts the filename from the Content-Disposition header or the  URL.
func getFilename(resp *http.Response) string {
	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if filename, ok := params["filename"]; ok {
				return filename
			} else if filename, ok := params["filename*"]; ok {
				return filename
			}
		}
	}

	// Fallback to URL-derived name
	u := resp.Request.URL
	base := path.Base(u.Path)
	if base != "" && base != "/" {
		return base
	}

	vals := u.Query()
	if filename := vals.Get("filename"); filename != "" {
		return filename
	}

	return defaultDownloadName
}

// parseLastModified parses the Last-Modified header.
func parseLastModified(header string) time.Time {
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
