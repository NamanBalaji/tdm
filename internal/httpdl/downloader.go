package httpdl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/logger"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

type Downloader struct {
	cfg    *config.HTTPConfig
	client *httpPkg.Client
}

func New(cfg *config.HTTPConfig) *Downloader {
	return &Downloader{
		cfg:    cfg,
		client: httpPkg.NewClient(),
	}
}

func (d *Downloader) Type() string { return "http" }

func (d *Downloader) CanHandle(url string) bool {
	return httpPkg.IsDownloadable(url)
}

func (d *Downloader) Init(ctx context.Context, url string, priority int) (*download.Download, error) {
	id := uuid.New()
	tempDir := filepath.Join(d.cfg.TempDir, id.String())

	meta, err := d.probe(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to probe URL: %w", err)
	}

	chunks := makeChunks(meta, tempDir, d.cfg.Chunks)

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	st := httpState{
		Chunks:         chunks,
		SupportsRanges: meta.supportsRanges,
		TempDir:        tempDir,
	}

	stateBytes, err := json.Marshal(&st)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &download.Download{
		ID:        id,
		URL:       url,
		Filename:  meta.filename,
		Dir:       d.cfg.DownloadDir,
		Status:    download.Pending,
		Priority:  priority,
		Type:      "http",
		TotalSize: meta.totalSize,
		State:     stateBytes,
	}, nil
}

func (d *Downloader) Start(ctx context.Context, dl *download.Download, onProgress func(int64, int64)) error {
	var st httpState
	if err := json.Unmarshal(dl.State, &st); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Find incomplete chunks
	var pending []int

	for i, c := range st.Chunks {
		if !c.Completed {
			pending = append(pending, i)
		}
	}

	if len(pending) == 0 {
		return nil
	}

	// Shared counter for progress reporting
	var totalDownloaded atomic.Int64
	for _, c := range st.Chunks {
		totalDownloaded.Add(c.Downloaded)
	}

	// Progress reporting goroutine
	reportCtx, reportCancel := context.WithCancel(ctx)
	reportDone := make(chan struct{})

	go func() {
		defer close(reportDone)

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-reportCtx.Done():
				return
			case <-ticker.C:
				onProgress(totalDownloaded.Load(), dl.TotalSize)
			}
		}
	}()

	// Download chunks concurrently with errgroup + semaphore
	g, gCtx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, d.cfg.Connections)

	for _, idx := range pending {
		chunkIdx := idx

		g.Go(func() error {
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			return d.downloadChunk(gCtx, &st.Chunks[chunkIdx], dl.URL, st.SupportsRanges, &totalDownloaded)
		})
	}

	err := g.Wait()

	// Stop progress reporter and wait for it to exit
	reportCancel()
	<-reportDone

	// Save state for resume (whether success, cancel, or error)
	d.saveState(dl, &st)

	if err != nil {
		return err
	}

	// All chunks complete — merge into final file
	if mergeErr := d.merge(&st, dl.Dir, dl.Filename); mergeErr != nil {
		return mergeErr
	}

	// Clean up temp directory
	_ = os.RemoveAll(st.TempDir)

	return nil
}

func (d *Downloader) Remove(dl *download.Download) error {
	var st httpState
	if err := json.Unmarshal(dl.State, &st); err == nil {
		_ = os.RemoveAll(st.TempDir)
	}

	_ = os.Remove(filepath.Join(dl.Dir, dl.Filename))

	return nil
}

// downloadChunk downloads a single chunk with retry logic.
func (d *Downloader) downloadChunk(ctx context.Context, chunk *chunkState, url string, supportsRanges bool, downloaded *atomic.Int64) error {
	var lastErr error

	for attempt := range d.cfg.MaxRetries {
		err := d.transferChunk(ctx, chunk, url, supportsRanges, downloaded)
		if err == nil {
			chunk.Completed = true

			return nil
		}

		lastErr = err
		if errors.Is(err, context.Canceled) || !isRetryableError(err) {
			return err
		}

		backoff := calculateBackoff(attempt, d.cfg.RetryDelay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			logger.Debugf("retrying chunk %s, attempt %d", chunk.ID, attempt+2)
		}
	}

	return fmt.Errorf("chunk %s failed after %d retries: %w", chunk.ID, d.cfg.MaxRetries, lastErr)
}

// transferChunk performs the actual byte transfer for a single chunk.
func (d *Downloader) transferChunk(ctx context.Context, chunk *chunkState, url string, supportsRanges bool, downloaded *atomic.Int64) error {
	currentStart := chunk.StartByte + chunk.Downloaded

	headers := map[string]string{"User-Agent": httpPkg.DefaultUserAgent}
	if supportsRanges {
		headers["Range"] = fmt.Sprintf("bytes=%d-%d", currentStart, chunk.EndByte)
	}

	conn := newConnection(url, headers, d.client, currentStart, chunk.EndByte)

	defer func() { _ = conn.close() }()

	file, err := os.OpenFile(chunk.TempFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open chunk file: %w", err)
	}

	defer func() { _ = file.Close() }()

	offset := chunk.Downloaded
	if !supportsRanges {
		offset = 0
	}

	if _, err := file.Seek(offset, 0); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	buf := make([]byte, 32*1024)
	totalSize := chunk.EndByte - chunk.StartByte + 1
	remaining := totalSize - chunk.Downloaded

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := conn.Read(ctx, buf)
		if n > 0 {
			writeN := min(int64(n), remaining)

			if _, err := file.Write(buf[:writeN]); err != nil {
				return fmt.Errorf("failed to write: %w", err)
			}

			chunk.Downloaded += writeN
			downloaded.Add(writeN)
			remaining -= writeN
		}

		if readErr != nil {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return readErr
			}

			if readErr.Error() == "EOF" || errors.Is(readErr, io.EOF) {
				break
			}

			if remaining <= 0 {
				break
			}

			return readErr
		}
	}

	return nil
}

// saveState serializes the current chunk progress back to dl.BackendState.
func (d *Downloader) saveState(dl *download.Download, st *httpState) {
	if data, err := json.Marshal(st); err == nil {
		dl.State = data
	}
}

func makeChunks(meta *probeResult, tempDir string, numChunks int) []chunkState {
	if meta.totalSize <= 0 {
		return nil
	}

	if !meta.supportsRanges {
		id := uuid.New()

		return []chunkState{{
			ID:           id,
			StartByte:    0,
			EndByte:      meta.totalSize - 1,
			TempFilePath: filepath.Join(tempDir, id.String()),
		}}
	}

	chunkSize := meta.totalSize / int64(numChunks)
	if chunkSize <= 0 {
		id := uuid.New()

		return []chunkState{{
			ID:           id,
			StartByte:    0,
			EndByte:      meta.totalSize - 1,
			TempFilePath: filepath.Join(tempDir, id.String()),
		}}
	}

	var (
		chunks []chunkState
		start  int64
	)

	for start < meta.totalSize {
		end := min(start+chunkSize-1, meta.totalSize-1)

		id := uuid.New()
		chunks = append(chunks, chunkState{
			ID:           id,
			StartByte:    start,
			EndByte:      end,
			TempFilePath: filepath.Join(tempDir, id.String()),
		})
		start = end + 1
	}

	return chunks
}
