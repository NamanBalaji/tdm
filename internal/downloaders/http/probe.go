package http

import (
	"context"
	"log/slog"
	"mime"
	"strconv"
	"strings"

	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func (d *Downloader) probe(ctx context.Context, url string) (*probeResult, error) {
	result, err := d.probeWithHEAD(ctx, url)
	if err == nil {
		return result, nil
	}

	slog.Warn("HEAD probe failed, falling back", "err", err)

	if !httpPkg.IsFallbackError(err) {
		return nil, err
	}

	result, err = d.probeWithRangeGET(ctx, url)
	if err == nil {
		return result, nil
	}

	slog.Warn("Range GET probe failed, falling back", "err", err)

	if !httpPkg.IsFallbackError(err) {
		return nil, err
	}

	return d.probeWithRegularGET(ctx, url)
}

func (d *Downloader) probeWithHEAD(ctx context.Context, url string) (*probeResult, error) {
	resp, err := d.client.Head(ctx, url, nil)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	return &probeResult{
		filename:       httpPkg.GetFilename(resp),
		contentType:    resp.Header.Get("Content-Type"),
		totalSize:      resp.ContentLength,
		supportsRanges: resp.Header.Get("Accept-Ranges") == "bytes",
	}, nil
}

func (d *Downloader) probeWithRangeGET(ctx context.Context, url string) (*probeResult, error) {
	resp, err := d.client.Range(ctx, url, 0, 0, nil)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	var totalSize int64

	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if _, sizeStr, ok := strings.CutLast(cr, "/"); ok {
			if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
				totalSize = size
			}
		}
	}

	return &probeResult{
		filename:       httpPkg.GetFilename(resp),
		contentType:    resp.Header.Get("Content-Type"),
		totalSize:      totalSize,
		supportsRanges: true,
	}, nil
}

func (d *Downloader) probeWithRegularGET(ctx context.Context, url string) (*probeResult, error) {
	resp, err := d.client.Get(ctx, url)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	return &probeResult{
		filename:       httpPkg.GetFilename(resp),
		contentType:    resp.Header.Get("Content-Type"),
		totalSize:      resp.ContentLength,
		supportsRanges: false,
	}, nil
}

// isTorrentContentType reports whether a Content-Type header value marks a
// .torrent (BitTorrent metainfo) file.
func isTorrentContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	return mediaType == "application/x-bittorrent" || mediaType == "application/torrent"
}
