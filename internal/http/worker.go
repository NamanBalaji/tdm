package http

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/status"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

var (
	ErrDirectoryCreateFailed = errors.New("directory create failed")
	ErrFileCreateFailed      = errors.New("file create failed")
	ErrFileWriteFailed       = errors.New("file write failed")
	ErrFileOpenFailed        = errors.New("file open failed")
	ErrFileCopyFailed        = errors.New("file copy failed")

	ErrChunksIncomplete    = errors.New("not all chunks completed successfully")
	ErrChunkDownloadFailed = errors.New("chunk download failed after max retries")
	ErrAlreadyStarted      = errors.New("download already started")
)

type Worker struct {
	repo     *repository.BboltRepository
	download *Download
	client   *httpPkg.Client
	config   *Config

	// Use atomic for thread-safe state management
	started  atomic.Bool
	finished atomic.Bool

	// Channels and context
	done     chan error
	cancel   context.CancelFunc
	cancelMu sync.Mutex

	progressMu   sync.RWMutex
	lastProgress Progress
}

func New(ctx context.Context, url string, downloadData *Download, repo *repository.BboltRepository, priority int, opts ...ConfigOption) (*Worker, error) {
	client := httpPkg.NewClient()

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	var (
		download     *Download
		lastProgress Progress
	)

	if downloadData == nil {
		d, err := NewDownload(ctx, url, client, cfg.MaxChunks, priority)
		if err != nil {
			return nil, err
		}

		download = d
	} else {
		download = downloadData

		download.mu = sync.RWMutex{}
		if download.Status == status.Queued || download.Status == status.Pending {
			download.setStatus(status.Paused)
		}

		var downloaded int64

		for _, chunk := range download.Chunks {
			chunk.mu = sync.RWMutex{}
			downloaded += chunk.Downloaded
		}

		total := download.TotalSize

		pct := 0.0
		if total > 0 {
			pct = float64(downloaded) / float64(total) * 100
		}

		if download.Status == status.Completed {
			pct = 100
		}

		lastProgress = Progress{
			TotalSize:  total,
			Downloaded: downloaded,
			Percentage: pct,
			SpeedBPS:   0,
			ETA:        0,
		}
	}

	err := repo.Save(download)
	if err != nil {
		return nil, fmt.Errorf("failed to save download: %w", err)
	}

	return &Worker{
		repo:         repo,
		download:     download,
		client:       client,
		done:         make(chan error, 1),
		config:       cfg,
		lastProgress: lastProgress,
	}, nil
}

// GetPriority returns the worker's priority.
func (w *Worker) GetPriority() int {
	return w.download.getPriority()
}

// GetID returns the download ID.
func (w *Worker) GetID() uuid.UUID {
	return w.download.GetID()
}

// GetStatus returns the current download status.
func (w *Worker) GetStatus() status.Status {
	return w.download.getStatus()
}

func (w *Worker) Start(ctx context.Context) error {
	if !w.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	w.finished.Store(false)

	s := w.download.getStatus()
	if s == status.Completed || s == status.Cancelled {
		w.started.Store(false)
		return nil
	}

	w.download.setStatus(status.Active)

	chunks := w.download.getDownloadableChunks()
	w.download.setStartTime(time.Now())

	if len(chunks) == 0 {
		w.finish(nil)
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	w.setCancel(cancel)

	go w.saveState(runCtx)
	go w.trackProgress(runCtx)
	go w.processDownload(runCtx, chunks)

	return nil
}

func (w *Worker) Cancel() error {
	return w.stop(status.Cancelled, false)
}

func (w *Worker) Pause() error {
	currentStatus := w.download.getStatus()
	if currentStatus != status.Active && currentStatus != status.Queued {
		return nil
	}

	return w.stop(status.Paused, false)
}

func (w *Worker) Resume(ctx context.Context) error {
	if w.download.getStatus() != status.Paused {
		return nil
	}

	w.download.setStatus(status.Pending)

	return w.Start(ctx)
}

func (w *Worker) Remove() error {
	return w.stop(status.Cancelled, true)
}

func (w *Worker) Done() <-chan error {
	return w.done
}

// Progress returns the current download progress.
func (w *Worker) Progress() progress.Progress {
	w.progressMu.RLock()
	defer w.progressMu.RUnlock()

	return w.lastProgress
}

// GetDownload returns the download info (for engine use).
func (w *Worker) GetDownload() *Download {
	return w.download
}

// GetFilename returns the filename of the download.
func (w *Worker) GetFilename() string {
	return w.download.Filename
}

// Queue sets the download status to Queued.
func (w *Worker) Queue() {
	w.download.setStatus(status.Queued)
}

func (w *Worker) setCancel(cancel context.CancelFunc) {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()

	w.cancel = cancel
}

func (w *Worker) getCancel() context.CancelFunc {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()

	return w.cancel
}

func (w *Worker) saveState(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if w.repo != nil {
				if err := w.repo.Save(w.download); err != nil {
					logger.Errorf("Failed to save download: %v", err)
				}
			}
		}
	}
}

// trackProgress periodically updates w.lastProgress with a smoothed speed/ETA.
func (w *Worker) trackProgress(ctx context.Context) {
	const (
		tickInterval    = 500 * time.Millisecond
		smoothingWindow = 5 * time.Second
	)

	type sample struct {
		t     time.Time
		bytes int64
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	var history []sample

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			var totalDownloaded int64
			for _, c := range w.download.getChunks() {
				totalDownloaded += c.getDownloaded()
			}

			w.download.setDownloaded(totalDownloaded)

			now := time.Now()
			history = append(history, sample{t: now, bytes: totalDownloaded})

			cutoff := now.Add(-smoothingWindow)
			for len(history) > 0 && history[0].t.Before(cutoff) {
				history = history[1:]
			}

			var speedBPS int64

			if len(history) >= 2 {
				oldest := history[0]

				elapsed := now.Sub(oldest.t).Seconds()
				if elapsed > 0 {
					speedBPS = int64(float64(totalDownloaded-oldest.bytes) / elapsed)
				}
			}

			totalSize := w.download.getTotalSize()

			var eta time.Duration

			if speedBPS > 0 && totalSize > 0 {
				remaining := totalSize - totalDownloaded
				if remaining > 0 {
					eta = time.Duration(float64(remaining)/float64(speedBPS)) * time.Second
				}
			}

			percentage := 0.0
			if totalSize > 0 {
				percentage = float64(totalDownloaded) / float64(totalSize) * 100
				if totalDownloaded >= totalSize {
					percentage = 100
				}
			}

			w.progressMu.Lock()
			w.lastProgress = Progress{
				TotalSize:  totalSize,
				Downloaded: totalDownloaded,
				Percentage: percentage,
				SpeedBPS:   speedBPS,
				ETA:        eta,
			}
			w.progressMu.Unlock()
		}
	}
}

func (w *Worker) processDownload(ctx context.Context, chunks []*Chunk) {
	g, groupCtx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, w.config.Connections)

	for _, currentChunk := range chunks {
		c := currentChunk

		g.Go(func() error {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			return w.downloadChunkWithRetries(groupCtx, c)
		})
	}

	err := g.Wait()
	if errors.Is(err, context.Canceled) {
		w.finish(nil)

		return
	}

	w.finish(err)
}

func (w *Worker) downloadChunkWithRetries(ctx context.Context, chunk *Chunk) error {
	var lastErr error

	for attempt := range w.config.MaxRetries {
		err := w.downloadChunk(ctx, chunk)
		if err == nil {
			return nil
		}

		lastErr = err
		if errors.Is(err, context.Canceled) || !isRetryableError(err) {
			return err
		}

		chunk.setRetryCount(int32(attempt + 1))
		backoff := calculateBackoff(attempt, w.config.RetryDelay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			logger.Debugf("Retrying chunk %s, attempt %d", chunk.ID, attempt+2)
		}
	}

	chunk.setStatus(status.Failed)
	logger.Errorf("Chunk %s failed after max retries: %v", chunk.ID, lastErr)

	return fmt.Errorf("%w: %w", ErrChunkDownloadFailed, lastErr)
}

func (w *Worker) downloadChunk(ctx context.Context, chunk *Chunk) error {
	currentStartByte := chunk.getStartByte() + chunk.getDownloaded()

	headers := make(map[string]string)
	headers["User-Agent"] = httpPkg.DefaultUserAgent

	// Only add range header if the download supports it
	if w.download.getSupportsRanges() {
		headers["Range"] = fmt.Sprintf("bytes=%d-%d", currentStartByte, chunk.getEndByte())
	}

	conn := newConnection(w.download.getURL(), headers, w.client, currentStartByte, chunk.getEndByte())

	defer func() {
		if err := conn.close(); err != nil {
			logger.Errorf("Failed to close connection for chunk %s: %v", chunk.ID, err)
		}
	}()

	return chunk.Download(ctx, conn, !w.download.getSupportsRanges())
}

func (w *Worker) finish(err error) {
	if !w.finished.CompareAndSwap(false, true) {
		return
	}

	defer func() {
		if cancel := w.getCancel(); cancel != nil {
			cancel()
		}

		if w.repo != nil {
			if err := w.repo.Save(w.download); err != nil {
				logger.Errorf("Failed to save download state on finish: %v", err)
			}
		}

		w.started.Store(false)

		select {
		case w.done <- err:
		default:
		}
	}()

	if err != nil {
		w.download.setStatus(status.Failed)
		return
	}

	s := w.download.getStatus()
	if s != status.Active {
		return
	}

	for _, chunk := range w.download.getChunks() {
		if chunk.getStatus() != status.Completed {
			w.download.setStatus(status.Failed)

			err = ErrChunksIncomplete

			return
		}
	}

	mergeErr := w.mergeChunks()
	if mergeErr != nil {
		w.download.setStatus(status.Failed)

		err = mergeErr

		return
	}

	w.download.setStatus(status.Completed)
	w.download.setEndTime(time.Now())

	total := w.download.getTotalSize()
	w.progressMu.Lock()
	w.lastProgress = Progress{
		TotalSize:  total,
		Downloaded: total,
		Percentage: 100,
		SpeedBPS:   0,
		ETA:        0,
	}
	w.progressMu.Unlock()

	if err := os.RemoveAll(w.download.getTempDir()); err != nil {
		logger.Errorf("Failed to remove temporary directory %s: %v", w.download.getTempDir(), err)
	}
}

func (w *Worker) mergeChunks() error {
	finalDir := w.download.getDir()

	err := os.MkdirAll(finalDir, 0755)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDirectoryCreateFailed, err)
	}

	targetPath := filepath.Join(finalDir, w.download.Filename)

	outFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileCreateFailed, err)
	}

	defer func() {
		if err := outFile.Close(); err != nil {
			logger.Errorf("Failed to close output file %s: %v", targetPath, err)
		}
	}()

	bufWriter := bufio.NewWriterSize(outFile, 4*1024*1024) // 4MB buffer

	defer func() {
		if err := bufWriter.Flush(); err != nil {
			logger.Errorf("Failed to flush buffer for file %s: %v", targetPath, err)
		}
	}()

	sortedChunks := w.download.getChunks()
	sort.Slice(sortedChunks, func(i, j int) bool {
		return sortedChunks[i].StartByte < sortedChunks[j].StartByte
	})

	totalBytes := int64(0)

	for _, c := range sortedChunks {
		chunkFile, err := os.Open(c.getTempFilePath())
		if err != nil {
			return fmt.Errorf("%w: %s", ErrFileOpenFailed, c.getTempFilePath())
		}

		bytesCopied, err := io.Copy(bufWriter, chunkFile)
		if fileCloseErr := chunkFile.Close(); fileCloseErr != nil {
			logger.Errorf("Failed to close file %s: %v", c.getTempFilePath(), fileCloseErr)
		}

		if err != nil {
			return fmt.Errorf("%w: %w", ErrFileCopyFailed, err)
		}

		totalBytes += bytesCopied
	}

	logger.Debugf("Flushing %d bytes to disk for file %s", totalBytes, targetPath)

	err = bufWriter.Flush()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFileWriteFailed, err)
	}

	return nil
}

func (w *Worker) stop(s status.Status, remove bool) error {
	currentStatus := w.download.getStatus()

	if currentStatus == status.Completed || currentStatus == status.Failed || currentStatus == status.Cancelled {
		if remove {
			w.cleanupFiles()
		}

		return nil
	}

	if s == status.Paused && currentStatus == status.Paused {
		return nil
	}

	w.download.setStatus(s)

	if cancel := w.getCancel(); cancel != nil {
		cancel()
	}

	for _, chunk := range w.download.getDownloadableChunks() {
		chunk.setStatus(s)
	}

	if remove {
		w.cleanupFiles()

		return nil
	}

	if w.repo != nil {
		if err := w.repo.Save(w.download); err != nil {
			return fmt.Errorf("failed to save download state: %w", err)
		}
	}

	return nil
}

func (w *Worker) cleanupFiles() {
	_ = os.RemoveAll(w.download.getTempDir())

	_ = os.Remove(filepath.Join(w.download.getDir(), w.download.Filename))
	if w.repo != nil {
		_ = w.repo.Delete(w.download.GetID().String())
	}
}
