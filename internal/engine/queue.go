package engine

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/google/uuid"
)

// PrioritizedDownload represents a download with its priority in the queue
type PrioritizedDownload struct {
	Download *downloader.Download
	Priority int
}

// QueueProcessor manages downloads with a priority queue system
type QueueProcessor struct {
	maxConcurrent int

	queuedDownloads []*PrioritizedDownload
	activeDownloads map[uuid.UUID]struct{}

	startDownloadFn func(context.Context, *downloader.Download) error

	completionCh chan uuid.UUID

	ctx  context.Context
	done chan struct{}
	mu   sync.Mutex
}

// NewQueueProcessor creates a new queue processor
func NewQueueProcessor(
	maxConcurrent int,
	startDownloadFn func(context.Context, *downloader.Download) error,
) *QueueProcessor {
	return &QueueProcessor{
		maxConcurrent:   maxConcurrent,
		queuedDownloads: make([]*PrioritizedDownload, 0),
		activeDownloads: make(map[uuid.UUID]struct{}),
		startDownloadFn: startDownloadFn,
		completionCh:    make(chan uuid.UUID, 10),
		done:            make(chan struct{}),
	}
}

// Start begins queue processing
func (q *QueueProcessor) Start(ctx context.Context) {
	q.ctx = ctx
	go q.processQueue()
}

// Stop stops the queue processor
func (q *QueueProcessor) Stop() {
	close(q.done)
}

// processQueue is the main loop that handles queue events
func (q *QueueProcessor) processQueue() {
	for {
		select {
		case completedID := <-q.completionCh:
			q.handleDownloadCompletion(completedID)
		case <-q.ctx.Done():
			return
		case <-q.done:
			return
		}
	}
}

// EnqueueDownload adds a download to the queue
func (q *QueueProcessor) EnqueueDownload(download *downloader.Download, priority int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	download.Status = common.StatusQueued

	q.queuedDownloads = append(q.queuedDownloads, &PrioritizedDownload{
		Download: download,
		Priority: priority,
	})

	slog.Info("Enqueued download", slog.Any("download", download))
	q.sortQueue()

	q.fillAvailableSlots()
}

// NotifyDownloadCompletion informs the queue when a download completes/fails/cancels
func (q *QueueProcessor) NotifyDownloadCompletion(downloadID uuid.UUID) {
	q.completionCh <- downloadID
}

// handleDownloadCompletion processes a download completion notification
func (q *QueueProcessor) handleDownloadCompletion(downloadID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.activeDownloads, downloadID)

	q.fillAvailableSlots()
}

// sortQueue sorts the queue by priority (higher first)
func (q *QueueProcessor) sortQueue() {
	sort.Slice(q.queuedDownloads, func(i, j int) bool {
		return q.queuedDownloads[i].Priority > q.queuedDownloads[j].Priority
	})
}

// fillAvailableSlots starts downloads if slots are available
func (q *QueueProcessor) fillAvailableSlots() {
	available := q.maxConcurrent - len(q.activeDownloads)

	if available <= 0 || len(q.queuedDownloads) == 0 {
		return
	}

	toStart := min(available, len(q.queuedDownloads))
	for i := 0; i < toStart; i++ {
		pd := q.queuedDownloads[0]

		q.queuedDownloads = q.queuedDownloads[1:]
		q.activeDownloads[pd.Download.ID] = struct{}{}

		download := pd.Download
		go func() {
			err := q.startDownloadFn(q.ctx, download)
			if err != nil {
				q.NotifyDownloadCompletion(download.ID)
			}
		}()
	}
}
