package downloader

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/logger"
)

// Start initiates the download process for the chunks
func (d *Download) Start(ctx context.Context, pool *connection.Pool) {
	if d.GetStatus() == common.StatusActive {
		return
	}

	d.mu.Lock()
	defer func() {
		close(d.done)
		d.mu.Unlock()
	}()
	d.SetStatus(common.StatusActive)
	d.StartTime = time.Now()

	chunks := d.getPendingChunks()
	if len(chunks) == 0 {
		logger.Debugf("No pending chunks found for download %s", d.ID)
		d.finishDownload()
	}

	d.processDownload(ctx, chunks, pool)
}

// processDownload downloads the chunks concurrently using a connection pool
func (d *Download) processDownload(ctx context.Context, chunks []*chunk.Chunk, pool *connection.Pool) {
	g, ctx := errgroup.WithContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	d.cancelFunc = cancel

	sem := make(chan struct{}, d.Config.Connections)
	for _, c := range chunks {
		c := c
		g.Go(func() error {
			select {
			case <-ctx.Done():
				logger.Debugf("Download cancelled for chunk %s", c.ID)
				return ctx.Err()
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			return d.downloadChunkWithRetries(ctx, c, pool)
		})
	}

	err := g.Wait()
	if err == nil {
		d.finishDownload()
		return
	}
	if errors.Is(err, context.Canceled) {
		if d.GetIsExternal() {
			d.SetStatus(common.StatusPaused)
		}
		return
	}

	d.handleDownloadFailure(err)
}

// getPendingChunks returns a list of chunks that are not completed
func (d *Download) getPendingChunks() []*chunk.Chunk {
	var pending []*chunk.Chunk
	for _, c := range d.Chunks {
		if c.GetStatus() != common.StatusCompleted {
			pending = append(pending, c)
		}
	}
	logger.Debugf("Found %d pending chunks for download %s", len(pending), d.ID)
	return pending
}

// downloadChunkWithRetries attempts to download a chunk with retries
func (d *Download) downloadChunkWithRetries(ctx context.Context, chunk *chunk.Chunk, pool *connection.Pool) error {
	err := d.downloadChunk(ctx, chunk, pool)
	if err == nil || errors.Is(err, context.Canceled) || !isRetryableError(err) {
		return err
	}

	for chunk.GetRetryCount() < d.Config.MaxRetries {
		chunk.Reset()

		retryCount := chunk.GetRetryCount()
		backoff := calculateBackoff(retryCount, d.Config.RetryDelay)
		logger.Debugf("Waiting %v before retrying chunk %s", backoff, chunk.ID)

		select {
		case <-time.After(backoff):
			err = d.downloadChunk(ctx, chunk, pool)
			if err == nil || errors.Is(err, context.Canceled) {
				return err
			}

			if !isRetryableError(err) {
				return err
			}

		case <-ctx.Done():
			logger.Debugf("Context cancelled while waiting to retry chunk %s", chunk.ID)
			chunk.SetStatus(common.StatusPaused)
			return err
		}
	}

	return err
}

// downloadChunk downloads a chunk using the provided connection pool
func (d *Download) downloadChunk(ctx context.Context, chunk *chunk.Chunk, pool *connection.Pool) error {
	conn, err := pool.GetConnection(ctx, d.URL, d.Config.Headers)
	if err != nil {
		return err
	}

	if conn == nil {
		conn, err = d.protocolHandler.CreateConnection(d.URL, chunk, d.Config)
		if err != nil {
			pool.ReleaseSlot(d.URL)
			return err
		}
		pool.RegisterConnection(conn)
	} else {
		d.protocolHandler.UpdateConnection(conn, chunk)
		if err := conn.Reset(ctx); err != nil {
			pool.ReleaseConnection(conn)
			return err
		}
	}

	chunk.SetConnection(conn)
	defer pool.ReleaseConnection(conn)

	return chunk.Download(ctx)
}

// finishDownload checks if all chunks are completed and merges them
func (d *Download) finishDownload() {
	for _, c := range d.Chunks {
		if c.GetStatus() != common.StatusCompleted {
			err := fmt.Errorf("cannot finish download: chunk %s is in state %s", c.ID, c.Status)
			d.handleDownloadFailure(err)
		}
	}

	targetPath := filepath.Join(d.Config.Directory, d.Filename)

	if err := d.chunkManager.MergeChunks(d.Chunks, targetPath); err != nil {
		logger.Errorf("Failed to merge chunks for download %s: %v", d.ID, err)
		d.handleDownloadFailure(err)
	}

	d.SetStatus(common.StatusCompleted)
	d.EndTime = time.Now()

	go func() {
		d.saveStateChan <- d
	}()
	if err := d.chunkManager.CleanupChunks(d.Chunks); err != nil {
		logger.Warnf("Failed to cleanup chunks for download %s: %s", d.ID, err)
	}
}

// handleDownloadFailure sets the status and error on a filed download
func (d *Download) handleDownloadFailure(err error) {
	d.SetStatus(common.StatusFailed)
	d.error = err
}

// calculateBackoff calculates a backoff duration with jitter.
func calculateBackoff(retryCount int, baseDelay time.Duration) time.Duration {
	// Exponential backoff: 2^retryCount * baseDelay
	delay := baseDelay * (1 << uint(retryCount))

	// Apply jitter to avoid thundering herd (between 75% and 125% of computed delay)
	jitterFactor := 0.75 + 0.5*rand.Float64()
	jitter := time.Duration(float64(delay) * jitterFactor)

	// Cap maximum delay at 2 minutes
	maxDelay := 2 * time.Minute
	if jitter > maxDelay {
		jitter = maxDelay
	}

	return jitter
}

// Stop stops the download process and cleans up resources
func (d *Download) Stop(status common.Status, removeFiles bool) {
	if d.GetStatus() != common.StatusActive && status == common.StatusPaused {
		return
	}

	d.cancelFunc()
	<-d.done
	d.SetStatus(status)

	go func() {
		d.saveStateChan <- d
	}()

	if removeFiles {
		if err := d.chunkManager.CleanupChunks(d.Chunks); err != nil {
			logger.Errorf("Failed to cleanup chunks for download %s: %s", d.ID, err)
		}
	}
}

// Remove deletes the output file and cleans up the chunks
func (d *Download) Remove() {
	if d.GetStatus() == common.StatusActive {
		d.Stop(common.StatusCancelled, true)
	}
	outputPath := filepath.Join(d.Config.Directory, d.Filename)
	if _, err := os.Stat(outputPath); err == nil {
		if err := os.Remove(outputPath); err != nil {
			logger.Warnf("Failed to remove output file: %v", err)
		}
	}
}

// Resume resumes a paused or failed download
func (d *Download) Resume(ctx context.Context) bool {
	if d.GetStatus() != common.StatusPaused && d.GetStatus() != common.StatusFailed {
		return false
	}
	d.done = make(chan struct{})

	return true
}
