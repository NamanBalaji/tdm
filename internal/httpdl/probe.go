package httpdl

import (
	"context"
	"strconv"
	"strings"

	"github.com/NamanBalaji/tdm/internal/logger"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func (d *Downloader) probe(ctx context.Context, url string) (*probeResult, error) {
	result, err := d.probeWithHEAD(ctx, url)
	if err == nil {
		return result, nil
	}

	logger.Warnf("HEAD probe failed, falling back: %v", err)

	if !httpPkg.IsFallbackError(err) {
		return nil, err
	}

	result, err = d.probeWithRangeGET(ctx, url)
	if err == nil {
		return result, nil
	}

	logger.Warnf("Range GET probe failed, falling back: %v", err)

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
		parts := strings.Split(cr, "/")
		if len(parts) == 2 {
			if size, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				totalSize = size
			}
		}
	}

	return &probeResult{
		filename:       httpPkg.GetFilename(resp),
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
		totalSize:      resp.ContentLength,
		supportsRanges: false,
	}, nil
}
