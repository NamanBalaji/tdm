package http

import (
	"context"
	"sync"
	"sync/atomic"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/config"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

var PartPrefix = partPrefix

var (
	IsRetryableError  = isRetryableError
	CalculateBackoff  = calculateBackoff
	MakeChunks        = makeChunks
	ParseContentRange = parseContentRange
)

type Connection = connection

// NewTestConnection creates a connection with no expected file size, so the
// size check is skipped.
func NewTestConnection(url string, headers map[string]string, client *httpPkg.Client, start, end int64) *Connection {
	return newConnection(url, headers, client, start, end, 0)
}

func NewTestConnectionWithTotal(url string, headers map[string]string, client *httpPkg.Client, start, end, totalSize int64) *Connection {
	return newConnection(url, headers, client, start, end, totalSize)
}

func NewDownloaderWithClient(cfg *config.HTTPConfig, client *httpPkg.Client) *Downloader {
	return &Downloader{
		cfg:    cfg,
		client: client,
	}
}

func (d *Downloader) TestMerge(st *HttpState, dir, filename string) (string, error) {
	return d.merge(context.Background(), st, dir, filename, uuid.New())
}

// TestDownloadChunk downloads a chunk with no expected file size, so the size
// check is skipped.
func (d *Downloader) TestDownloadChunk(ctx context.Context, chunk *ChunkState, url string, supportsRanges bool, downloaded *atomic.Int64) error {
	return d.TestDownloadChunkWithTotal(ctx, chunk, url, supportsRanges, 0, downloaded)
}

func (d *Downloader) TestDownloadChunkWithTotal(ctx context.Context, chunk *ChunkState, url string, supportsRanges bool, totalSize int64, downloaded *atomic.Int64) error {
	var mu sync.Mutex

	return d.downloadChunk(ctx, chunk, url, supportsRanges, totalSize, downloaded, &mu)
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
