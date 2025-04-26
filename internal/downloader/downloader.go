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

// Start initiates the download process for the chunks.
func (d *Download) Start(ctx context.Context, pool *connection.Pool) {
	if d.GetStatus() == common.StatusActive {
		logger.Debugf("Download %s already active, returning.", d.ID)
		return
	}

	d.mu.Lock()

	if d.GetStatus() == common.StatusActive {
		d.mu.Unlock()
		logger.Debugf("Download %s became active concurrently, returning.", d.ID)
		return
	}

	d.SetStatus(common.StatusActive)
	d.StartTime = time.Now()
	d.error = nil
	d.ErrorMessage = ""

	if d.done == nil {
		d.done = make(chan struct{})
	}

	runCtx, cancel := context.WithCancel(ctx)
	d.cancelFunc = cancel

	chunksToProcess := make([]*chunk.Chunk, 0, len(d.Chunks))
	for _, c := range d.Chunks {
		if c.GetStatus() != common.StatusCompleted {
			chunksToProcess = append(chunksToProcess, c)
		}
	}

	if len(chunksToProcess) == 0 {
		logger.Debugf("No pending chunks found for download %s", d.ID)
		d.mu.Unlock()
		d.finishDownload()
		return
	}

	maxConns := d.Config.Connections
	downloadID := d.ID

	d.mu.Unlock()

	logger.Debugf("Starting download process for %s with %d pending chunks", downloadID, len(chunksToProcess))
	d.processDownload(runCtx, chunksToProcess, pool, maxConns)
}

// processDownload downloads the chunks concurrently using a connection pool.
func (d *Download) processDownload(runCtx context.Context, chunks []*chunk.Chunk, pool *connection.Pool, maxConns int) {
	defer func() {
		d.mu.Lock()
		if d.done != nil {
			close(d.done)
			d.done = nil
		}
		d.mu.Unlock()
	}()

	g, groupCtx := errgroup.WithContext(runCtx)
	sem := make(chan struct{}, maxConns)

	for _, currentChunk := range chunks {
		c := currentChunk

		g.Go(func() error {
			select {
			case <-groupCtx.Done():
				logger.Debugf("Context cancelled before starting chunk %s", c.ID)
				if errors.Is(groupCtx.Err(), context.Canceled) && d.GetStatus() != common.StatusActive {
					c.SetStatus(common.StatusPaused)
				}
				return groupCtx.Err()
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			err := d.downloadChunkWithRetries(groupCtx, c, pool)
			if err != nil {
				if errors.Is(err, context.Canceled) && groupCtx.Err() != nil {
					logger.Debugf("Download cancelled for chunk %s", c.ID)
					d.mu.RLock()
					isStopping := d.GetStatus() != common.StatusActive
					d.mu.RUnlock()
					if isStopping {
						c.SetStatus(common.StatusPaused)
					}
					return groupCtx.Err()
				}
				if !errors.Is(err, context.Canceled) {
					logger.Errorf("Error downloading chunk %s: %v", c.ID, err)
				}
				return err
			}
			return nil
		})
	}

	err := g.Wait()

	if err == nil {
		d.finishDownload()
	} else if errors.Is(err, context.Canceled) {
		logger.Infof("Download %s cancelled or stopped.", d.ID)
	} else {
		logger.Errorf("Download %s failed: %v", d.ID, err)
		d.handleDownloadFailure(err)
	}
}

// downloadChunkWithRetries attempts to download a chunk with retries.
func (d *Download) downloadChunkWithRetries(ctx context.Context, chunk *chunk.Chunk, pool *connection.Pool) error {
	d.mu.RLock()
	maxRetries := d.Config.MaxRetries
	retryDelay := d.Config.RetryDelay
	d.mu.RUnlock()

	err := d.downloadChunk(ctx, chunk, pool)
	if err == nil || errors.Is(err, context.Canceled) || !isRetryableError(err) {
		return err
	}

	for chunk.GetRetryCount() < maxRetries {
		retryCount := chunk.GetRetryCount()
		backoff := calculateBackoff(retryCount, retryDelay)
		logger.Debugf("Waiting %v before retrying chunk %s (attempt %d/%d)", backoff, chunk.ID, retryCount+1, maxRetries)

		select {
		case <-time.After(backoff):
			err = d.downloadChunk(ctx, chunk, pool)
			if err == nil || errors.Is(err, context.Canceled) || !isRetryableError(err) {
				return err
			}

		case <-ctx.Done():
			logger.Debugf("Context cancelled while waiting to retry chunk %s", chunk.ID)
			return ctx.Err()
		}
	}

	logger.Errorf("Chunk %s failed after %d retries: %v", chunk.ID, maxRetries, err)
	chunk.SetStatus(common.StatusFailed)
	return fmt.Errorf("chunk %s failed after %d retries: %w", chunk.ID, maxRetries, err)
}

// downloadChunk downloads a single chunk using the provided connection pool.
func (d *Download) downloadChunk(ctx context.Context, chunk *chunk.Chunk, pool *connection.Pool) error {
	d.mu.RLock()
	url := d.URL
	config := d.Config
	handler := d.protocolHandler
	d.mu.RUnlock()

	if handler == nil {
		return errors.New("protocol handler is nil")
	}

	conn, err := pool.GetConnection(ctx, url, config.Headers)
	if err != nil {
		return fmt.Errorf("failed to get connection from pool for chunk %s: %w", chunk.ID, err)
	}

	if conn == nil {
		conn, err = handler.CreateConnection(url, chunk, config)
		if err != nil {
			pool.ReleaseSlot(url)
			return fmt.Errorf("failed to create connection for chunk %s: %w", chunk.ID, err)
		}
		pool.RegisterConnection(conn)
	} else {
		handler.UpdateConnection(conn, chunk)
		if err := conn.Reset(ctx); err != nil {
			pool.ReleaseConnection(conn)
			return fmt.Errorf("failed to reset connection for chunk %s: %w", chunk.ID, err)
		}
	}

	chunk.SetConnection(conn)
	defer pool.ReleaseConnection(conn)

	return chunk.Download(ctx)
}

// finishDownload checks if all chunks are completed and merges them.
func (d *Download) finishDownload() {
	d.mu.RLock()
	chunks := make([]*chunk.Chunk, len(d.Chunks))
	copy(chunks, d.Chunks)
	d.mu.RUnlock()

	for _, c := range chunks {
		if c.GetStatus() != common.StatusCompleted {
			d.handleDownloadFailure(
				fmt.Errorf("cannot finish download: chunk %s is in state %s",
					c.ID, c.GetStatus()),
			)
			return
		}
	}

	targetPath := filepath.Join(d.Config.Directory, d.Filename)
	if err := d.chunkManager.MergeChunks(chunks, targetPath); err != nil {
		logger.Errorf("Failed to merge chunks for download %s: %v", d.ID, err)
		d.handleDownloadFailure(fmt.Errorf("merge failed: %w", err))
		return
	}

	d.mu.Lock()
	if d.GetStatus() == common.StatusActive {
		d.SetStatus(common.StatusCompleted)
		d.EndTime = time.Now()
		select {
		case d.saveStateChan <- d:
		default:
			logger.Warnf("Save channel full, skipping save for download %s", d.ID)
		}
	}
	d.mu.Unlock()

	if err := d.chunkManager.CleanupChunks(chunks); err != nil {
		logger.Warnf("Failed to cleanup chunks for download %s: %s", d.ID, err)
	}
}

// handleDownloadFailure sets the status and error on a failed download.
func (d *Download) handleDownloadFailure(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.GetStatus() == common.StatusActive {
		d.SetStatus(common.StatusFailed)
		d.error = err
		d.ErrorMessage = err.Error()
		logger.Errorf("Download %s failed with error: %v", d.ID, err)

		select {
		case d.saveStateChan <- d:
		default:
			logger.Warnf("Save channel full, could not save failed state for download %s", d.ID)
		}
	} else {
		logger.Errorf("Download %s encountered error while in status %s: %v", d.ID, d.GetStatus(), err)
	}
}

// calculateBackoff calculates a backoff duration with jitter.
func calculateBackoff(retryCount int, baseDelay time.Duration) time.Duration {
	delay := baseDelay * (1 << uint(retryCount))

	jitterFactor := 0.75 + 0.5*rand.Float64()
	jitter := time.Duration(float64(delay) * jitterFactor)

	maxDelay := 2 * time.Minute
	if jitter > maxDelay {
		jitter = maxDelay
	}

	return jitter
}

// Stop stops the download process and cleans up resources.
func (d *Download) Stop(status common.Status, removeFiles bool) {
	d.mu.Lock()
	currentStatus := d.GetStatus()

	if status != common.StatusCancelled &&
		(currentStatus == common.StatusPaused ||
			currentStatus == common.StatusCancelled ||
			currentStatus == common.StatusCompleted ||
			currentStatus == common.StatusFailed) {
		logger.Debugf("Stop called for download %s, but status is already %s. Ignoring.", d.ID, currentStatus)
		d.mu.Unlock()
		return
	}

	if currentStatus != common.StatusActive && status == common.StatusPaused {
		logger.Debugf("Stop called with Pause for download %s, but status is %s. Ignoring.", d.ID, currentStatus)
		d.mu.Unlock()
		return
	}

	cancel := d.cancelFunc
	doneChan := d.done // Copy channel reference
	chunksToClean := make([]*chunk.Chunk, len(d.Chunks))
	copy(chunksToClean, d.Chunks)
	chunkMgr := d.chunkManager
	downloadID := d.ID

	d.mu.Unlock()

	logger.Infof("Stopping download %s with status %s", downloadID, status)

	if cancel != nil {
		cancel()
	}

	if doneChan != nil {
		<-doneChan
		d.SetStatus(status)
		logger.Debugf("Download %s processDownload finished after stop.", downloadID)
	}

	select {
	case d.saveStateChan <- d:
	default:
		logger.Warnf("Save channel full, could not save final state for stopped download %s", downloadID)
	}

	if removeFiles {
		if err := chunkMgr.CleanupChunks(chunksToClean); err != nil {
			logger.Errorf("Failed to cleanup chunks for stopped download %s: %s", downloadID, err)
		}
	}
}

// Remove deletes the output file and cleans up the chunks.
func (d *Download) Remove() {
	if d.GetStatus() == common.StatusActive {
		d.Stop(common.StatusCancelled, true)
	} else {
		d.mu.RLock()
		chunksToClean := make([]*chunk.Chunk, len(d.Chunks))
		copy(chunksToClean, d.Chunks)
		chunkMgr := d.chunkManager
		d.mu.RUnlock()
		if chunkMgr != nil {
			if err := chunkMgr.CleanupChunks(chunksToClean); err != nil {
				logger.Errorf("Failed to cleanup chunks during Remove for non-active download %s: %s", d.ID, err)
			}
		}
	}

	d.mu.RLock()
	targetPath := filepath.Join(d.Config.Directory, d.Filename)
	downloadID := d.ID
	d.mu.RUnlock()

	if _, err := os.Stat(targetPath); err == nil {
		logger.Infof("Removing output file %s for download %s", targetPath, downloadID)
		if err := os.Remove(targetPath); err != nil {
			logger.Warnf("Failed to remove output file %s: %v", targetPath, err)
		}
	} else if !os.IsNotExist(err) {
		logger.Warnf("Failed to stat output file %s before removal: %v", targetPath, err)
	}
}

// Resume resumes a paused or failed download.
func (d *Download) Resume(ctx context.Context) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	currentStatus := d.GetStatus()
	if currentStatus != common.StatusPaused && currentStatus != common.StatusFailed {
		logger.Debugf("Cannot resume download %s from status %s", d.ID, currentStatus)
		return false
	}

	logger.Infof("Resuming download %s from status %s", d.ID, currentStatus)

	d.done = make(chan struct{})
	d.error = nil

	return true
}
