package httpdl

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
	internal := &httpState{
		Chunks:         st.Chunks,
		SupportsRanges: st.SupportsRanges,
		TempDir:        st.TempDir,
	}
	return d.merge(internal, dir, filename)
}

func (d *Downloader) TestDownloadChunk(ctx context.Context, chunk *ChunkState, url string, supportsRanges bool, downloaded *atomic.Int64) error {
	internal := &chunkState{
		ID:           chunk.ID,
		StartByte:    chunk.StartByte,
		EndByte:      chunk.EndByte,
		Downloaded:   chunk.Downloaded,
		Completed:    chunk.Completed,
		TempFilePath: chunk.TempFilePath,
	}
	err := d.downloadChunk(ctx, internal, url, supportsRanges, downloaded)
	chunk.Downloaded = internal.Downloaded
	chunk.Completed = internal.Completed
	return err
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
