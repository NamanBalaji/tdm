package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// StartDownload initiates a download
func (e *Engine) StartDownload(ctx context.Context, id uuid.UUID) error {
	download, err := e.GetDownload(id)
	if err != nil {
		return err
	}

	stats := download.GetStats()
	if stats.Status == common.StatusActive {
		return nil
	}

	downloadCtx, cancelFunc := context.WithCancel(ctx)
	download.SetContext(downloadCtx, cancelFunc)

	// Update status
	download.Status = common.StatusActive
	download.StartTime = time.Now()

	// save download

	go e.processDownload(download)

	return nil
}

// processDownload handles the actual download process
func (e *Engine) processDownload(download *downloader.Download) {
	chunks := e.getPendingChunks(download)

	// If all chunks are already complete, just finish the download
	if len(chunks) == 0 {
		if err := e.finishDownload(download); err != nil {
			log.Printf("error finishing download: %s", err)
		}
		return
	}

	g, ctx := errgroup.WithContext(download.Context())

	sem := make(chan struct{}, e.config.MaxConnectionsPerDownload)

	// Launch each chunk download
	for _, chunk := range chunks {
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return ctx.Err()
			}

			return e.downloadChunkWithRetries(ctx, download, chunk)
		})
	}

	err := g.Wait()

	if err != nil {
		if !errors.Is(err, context.Canceled) {
			e.handleDownloadFailure(download, err)
		}
	} else {
		if err := e.finishDownload(download); err != nil {
			log.Printf("error finishing download: %s", err)
		}
	}
}

// downloadChunkWithRetries downloads a chunk with retry logic
func (e *Engine) downloadChunkWithRetries(ctx context.Context, download *downloader.Download, chunk *chunk.Chunk) error {
	err := e.downloadChunk(ctx, download, chunk)

	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}

	for chunk.RetryCount < download.Options.MaxRetries {
		chunk.Reset()

		// Calculate backoff delay
		backoff := time.Duration(1<<uint(chunk.RetryCount)) * time.Second

		// Wait for backoff period or cancellation
		select {
		case <-time.After(backoff):
			// Try again
			err = e.downloadChunk(ctx, download, chunk)
			if err == nil || errors.Is(err, context.Canceled) {
				return err
			}
		case <-ctx.Done():
			chunk.Status = common.StatusPaused
			return ctx.Err()
		}
	}

	return fmt.Errorf("chunk %s failed after %d attempts: %w", chunk.ID, download.Options.MaxRetries, err)
}

// downloadChunk downloads a single chunk
func (e *Engine) downloadChunk(ctx context.Context, download *downloader.Download, chunk *chunk.Chunk) error {
	chunk.Status = common.StatusActive

	handler, err := e.protocolHandler.GetHandler(download.URL)
	if err != nil {
		chunk.Status = common.StatusFailed
		chunk.Error = err
		return fmt.Errorf("failed to get protocol handler: %w", err)
	}

	conn, err := handler.CreateConnection(download.URL, chunk, &download.Options)
	if err != nil {
		chunk.Status = common.StatusFailed
		chunk.Error = err
		return fmt.Errorf("failed to create connection: %w", err)
	}

	// Register with connection pool
	e.connectionPool.RegisterConnection(conn)
	defer e.connectionPool.ReleaseConnection(conn)

	chunk.Connection = conn
	return chunk.Download(ctx)
}

// getPendingChunks returns chunks that need downloading
func (e *Engine) getPendingChunks(download *downloader.Download) []*chunk.Chunk {
	var pending []*chunk.Chunk
	for _, chunk := range download.Chunks {
		if chunk.Status != common.StatusCompleted {
			pending = append(pending, chunk)
		}
	}
	return pending
}

// handleDownloadFailure updates download state on failure
func (e *Engine) handleDownloadFailure(download *downloader.Download, err error) {
	// Update status
	download.Status = common.StatusFailed
	download.Error = err

	// Save failed state
}

// finishDownload completes a successful download
func (e *Engine) finishDownload(download *downloader.Download) error {
	// Verify all chunks are complete
	for _, chunk := range download.Chunks {
		if chunk.Status != common.StatusCompleted {
			err := fmt.Errorf("cannot finish download: chunk %s is in state %s",
				chunk.ID, chunk.Status)
			e.handleDownloadFailure(download, err)
			return err
		}
	}

	targetPath := filepath.Join(download.Options.Directory, download.Filename)

	if err := e.chunkManager.MergeChunks(download.Chunks, targetPath); err != nil {
		e.handleDownloadFailure(download, err)
		return fmt.Errorf("failed to merge chunks: %w", err)
	}

	download.Status = common.StatusCompleted
	download.EndTime = time.Now()

	if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
		log.Printf("Warning: Failed to clean up chunks: %v", err)
	}

	// Save completed state

	return nil
}
