package engine

import (
	"context"
	"github.com/NamanBalaji/tdm/internal/logger"
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
	logger.Debug("Creating new queue processor with maxConcurrent=%d", maxConcurrent)

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
	logger.Debug("Starting queue processor")
	q.ctx = ctx
	go q.processQueue()
}

// Stop stops the queue processor
func (q *QueueProcessor) Stop() {
	logger.Debug("Stopping queue processor")
	close(q.done)
}

// processQueue is the main loop that handles queue events
func (q *QueueProcessor) processQueue() {
	logger.Debug("Queue processor main loop started")

	for {
		select {
		case completedID := <-q.completionCh:
			logger.Debug("Queue processor received completion notification for download %s", completedID)
			q.handleDownloadCompletion(completedID)
		case <-q.ctx.Done():
			logger.Debug("Queue processor stopping due to context cancellation")
			return
		case <-q.done:
			logger.Debug("Queue processor stopping due to done signal")
			return
		}
	}
}

// EnqueueDownload adds a download to the queue
func (q *QueueProcessor) EnqueueDownload(download *downloader.Download, priority int) {
	logger.Info("Enqueueing download %s with priority %d", download.ID, priority)

	q.mu.Lock()
	defer q.mu.Unlock()

	download.Status = common.StatusQueued
	logger.Debug("Setting download %s status to %s", download.ID, download.Status)

	q.queuedDownloads = append(q.queuedDownloads, &PrioritizedDownload{
		Download: download,
		Priority: priority,
	})

	logger.Info("Enqueued download %s: %s", download.ID, download.URL)
	q.sortQueue()

	currentActive := len(q.activeDownloads)
	currentQueued := len(q.queuedDownloads)
	logger.Debug("Queue status after enqueue: active=%d, queued=%d, maxConcurrent=%d",
		currentActive, currentQueued, q.maxConcurrent)

	q.fillAvailableSlots()
}

// NotifyDownloadCompletion informs the queue when a download completes/fails/cancels
func (q *QueueProcessor) NotifyDownloadCompletion(downloadID uuid.UUID) {
	logger.Debug("Notifying queue of download completion: %s", downloadID)
	q.completionCh <- downloadID
}

// handleDownloadCompletion processes a download completion notification
func (q *QueueProcessor) handleDownloadCompletion(downloadID uuid.UUID) {
	logger.Debug("Handling download completion for %s", downloadID)

	q.mu.Lock()
	defer q.mu.Unlock()

	_, exists := q.activeDownloads[downloadID]
	if !exists {
		logger.Warn("Download %s not found in active downloads map", downloadID)
	}

	delete(q.activeDownloads, downloadID)
	logger.Debug("Removed download %s from active downloads", downloadID)

	activeCount := len(q.activeDownloads)
	queuedCount := len(q.queuedDownloads)
	logger.Debug("Queue status after completion: active=%d, queued=%d, maxConcurrent=%d",
		activeCount, queuedCount, q.maxConcurrent)

	q.fillAvailableSlots()
}

// sortQueue sorts the queue by priority (higher first)
func (q *QueueProcessor) sortQueue() {
	logger.Debug("Sorting queue with %d downloads by priority", len(q.queuedDownloads))

	sort.Slice(q.queuedDownloads, func(i, j int) bool {
		return q.queuedDownloads[i].Priority > q.queuedDownloads[j].Priority
	})

	if len(q.queuedDownloads) > 0 {
		// Log the highest priority downloads for debugging
		maxToLog := min(3, len(q.queuedDownloads))
		for i := 0; i < maxToLog; i++ {
			logger.Debug("Queue position %d: download=%s, priority=%d",
				i+1, q.queuedDownloads[i].Download.ID, q.queuedDownloads[i].Priority)
		}
	}
}

// fillAvailableSlots starts downloads if slots are available
func (q *QueueProcessor) fillAvailableSlots() {
	available := q.maxConcurrent - len(q.activeDownloads)
	logger.Debug("Checking for available slots: active=%d, max=%d, available=%d, queued=%d",
		len(q.activeDownloads), q.maxConcurrent, available, len(q.queuedDownloads))

	if available <= 0 || len(q.queuedDownloads) == 0 {
		if available <= 0 {
			logger.Debug("No available slots")
		}
		if len(q.queuedDownloads) == 0 {
			logger.Debug("No downloads in queue")
		}
		return
	}

	toStart := min(available, len(q.queuedDownloads))
	logger.Debug("Starting %d download(s) to fill available slots", toStart)

	for i := 0; i < toStart; i++ {
		pd := q.queuedDownloads[0]
		downloadID := pd.Download.ID
		priority := pd.Priority

		logger.Debug("Dequeuing download %s (priority %d) for start", downloadID, priority)
		q.queuedDownloads = q.queuedDownloads[1:]
		q.activeDownloads[downloadID] = struct{}{}
		logger.Debug("Added download %s to active downloads map", downloadID)

		download := pd.Download
		go func() {
			logger.Debug("Starting download %s", download.ID)
			err := q.startDownloadFn(q.ctx, download)
			if err != nil {
				logger.Error("Download %s failed to start: %v", download.ID, err)
				q.NotifyDownloadCompletion(download.ID)
			}
		}()
	}

	logger.Debug("After filling slots: active=%d, queued=%d",
		len(q.activeDownloads), len(q.queuedDownloads))
}
