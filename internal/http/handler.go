package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/logger"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

// Handler implements the Protocol interface for HTTP/HTTPS.
type Handler struct {
	client *httpPkg.Client
}

// NewHandler creates a new HTTP protocol handler.
func NewHandler() *Handler {
	return &Handler{
		client: httpPkg.NewClient(),
	}
}

func (h *Handler) CanHandle(urlStr string) bool {
	return httpPkg.IsDownloadable(urlStr)
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

	if httpPkg.IsFallbackError(err) {
		logger.Debugf("Falling back to Range GET request for %s", urlStr)
		info, err = h.initializeWithRangeGET(ctx, urlStr, config)
		if err == nil {
			logger.Debugf("Range GET request successful for %s", urlStr)
			return info, nil
		}
		logger.Debugf("Range GET request failed for %s: %v", urlStr, err)
	}

	if httpPkg.IsFallbackError(err) {
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
	headers["User-Agent"] = httpPkg.DefaultUserAgent

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

	resp, err := h.client.Head(ctx, urlStr, config.Headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	supportsRanges := resp.Header.Get("Accept-Ranges") == "bytes"
	logger.Debugf("HEAD request successful, content-length=%d, supports-ranges=%v",
		resp.ContentLength, supportsRanges)

	return generateInfo(resp, supportsRanges, resp.ContentLength), nil
}

// initializeWithRangeGET tries to get file info using Range headers.
func (h *Handler) initializeWithRangeGET(ctx context.Context, urlStr string, config *common.Config) (*common.DownloadInfo, error) {
	logger.Debugf("Initializing with Range GET request: %s", urlStr)

	resp, err := h.client.Range(ctx, urlStr, 0, 0, config.Headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	contentRange := resp.Header.Get("Content-Range")
	var totalSize int64 = 0
	if contentRange != "" {
		// Format: bytes 0-0/1234
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			size, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				logger.Warnf("Failed to parse size from Content-Range header: %s", contentRange)
				return nil, httpPkg.ErrInvalidContentRange
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

	resp, err := h.client.Get(ctx, urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	logger.Debugf("Regular GET request successful, content-length=%d", resp.ContentLength)
	return generateInfo(resp, false, resp.ContentLength), nil
}

// generateInfo generates download info from the response.
func generateInfo(resp *http.Response, canRange bool, totalSize int64) *common.DownloadInfo {
	logger.Debugf("Generating download info for %s", resp.Request.URL)

	info := &common.DownloadInfo{
		URL:             resp.Request.URL.String(),
		MimeType:        resp.Header.Get("Content-Type"),
		TotalSize:       totalSize,
		SupportsRanges:  canRange,
		LastModified:    httpPkg.ParseLastModified(resp.Header.Get("Last-Modified")),
		ETag:            resp.Header.Get("ETag"),
		AcceptRanges:    canRange,
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Server:          resp.Header.Get("Server"),
		CanBeResumed:    canRange,
		Filename:        httpPkg.GetFilename(resp),
	}

	logger.Debugf("Download info generated: filename=%s, size=%d, supports-ranges=%v, type=%s",
		info.Filename, info.TotalSize, info.SupportsRanges, info.MimeType)

	return info
}

func (h *Handler) GetChunkManager(downloadID uuid.UUID, tempDir string) (chunk.Manager, error) {
	return NewChunkManager(downloadID.String(), tempDir)
}
