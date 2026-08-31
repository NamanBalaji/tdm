package http

import (
	"context"
	"sync/atomic"

	"github.com/NamanBalaji/tdm/internal/config"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

var (
	IsRetryableError = isRetryableError
	CalculateBackoff = calculateBackoff
	MakeChunks       = makeChunks
)

type Connection = connection

func NewTestConnection(url string, headers map[string]string, client *httpPkg.Client, start, end int64) *Connection {
	return newConnection(url, headers, client, start, end)
}

func NewDownloaderWithClient(cfg *config.HTTPConfig, client *httpPkg.Client) *Downloader {
	return &Downloader{
		cfg:    cfg,
		client: client,
	}
}

func (d *Downloader) TestMerge(st *HttpState, dir, filename string) error {
	return d.merge(st, dir, filename)
}

func (d *Downloader) TestDownloadChunk(ctx context.Context, chunk *ChunkState, url string, supportsRanges bool, downloaded *atomic.Int64) error {
	return d.downloadChunk(ctx, chunk, url, supportsRanges, downloaded)
}

type HttpState = httpState
type ChunkState = chunkState
type ProbeResult = probeResult

func NewProbeResult(filename string, totalSize int64, supportsRanges bool) *ProbeResult {
	return &probeResult{
		filename:       filename,
		totalSize:      totalSize,
		supportsRanges: supportsRanges,
	}
}

type TestProbeResult struct {
	Filename       string
	TotalSize      int64
	SupportsRanges bool
}

func (d *Downloader) TestProbe(ctx context.Context, url string) (*TestProbeResult, error) {
	r, err := d.probe(ctx, url)
	if err != nil {
		return nil, err
	}
	return &TestProbeResult{
		Filename:       r.filename,
		TotalSize:      r.totalSize,
		SupportsRanges: r.supportsRanges,
	}, nil
}
