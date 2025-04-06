package engine

import (
	"context"
	"sort"
	"sync"

	"github.com/NamanBalaji/tdm/internal/logger"

	"github.com/google/uuid"
)

// PrioritizedDownload represents a download with its priority in the queue
type PrioritizedDownload struct {
	Id       uuid.UUID
	Priority int
}

// QueueProcessor manages downloads with a priority queue system
type QueueProcessor struct {
	maxConcurrent int

	queuedDownloads []*PrioritizedDownload
	activeDownloads map[uuid.UUID]struct{}

	startDownloadFn func(id uuid.UUID) error

	completionCh chan uuid.UUID

	done chan struct{}
	mu   sync.Mutex
}

// NewQueueProcessor creates a new queue processor
func NewQueueProcessor(maxConcurrent int, startDownloadFn func(id uuid.UUID) error) *QueueProcessor {
	logger.Debugf("Creating new queue processor with maxConcurrent=%d", maxConcurrent)

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
	logger.Debugf("Starting queue processor")
	go q.processQueue(ctx)
}

// Stop stops the queue processor
func (q *QueueProcessor) Stop() {
	logger.Debugf("Stopping queue processor")
	close(q.done)
}

// processQueue is the main loop that handles queue events
func (q *QueueProcessor) processQueue(ctx context.Context) {
	logger.Debugf("Queue processor main loop started")

	for {
		select {
		case completedID := <-q.completionCh:
			logger.Debugf("Queue processor received completion notification for download %s", completedID)
			q.handleDownloadCompletion(completedID)
		case <-ctx.Done():
			logger.Debugf("Queue processor stopping due to context cancellation")
			return
		case <-q.done:
			logger.Debugf("Queue processor stopping due to done signal")
			return
		}
	}
}

// EnqueueDownload adds a download to the queue
func (q *QueueProcessor) EnqueueDownload(id uuid.UUID, priority int) {
	logger.Infof("Enqueueing download %s with priority %d", id, priority)

	q.mu.Lock()
	defer q.mu.Unlock()

	q.queuedDownloads = append(q.queuedDownloads, &PrioritizedDownload{
		Id:       id,
		Priority: priority,
	})

	logger.Infof("Enqueued download_id: %s", id)
	q.sortQueue()

	currentActive := len(q.activeDownloads)
	currentQueued := len(q.queuedDownloads)
	logger.Debugf("Queue status after enqueue: active=%d, queued=%d, maxConcurrent=%d",
		currentActive, currentQueued, q.maxConcurrent)

	q.fillAvailableSlots()
}

// NotifyDownloadCompletion informs the queue when a download completes/fails/cancels
func (q *QueueProcessor) NotifyDownloadCompletion(downloadID uuid.UUID) {
	logger.Debugf("Notifying queue of download completion: %s", downloadID)
	q.completionCh <- downloadID
}

// handleDownloadCompletion processes a download completion notification
func (q *QueueProcessor) handleDownloadCompletion(downloadID uuid.UUID) {
	logger.Debugf("Handling download completion for %s", downloadID)

	q.mu.Lock()
	defer q.mu.Unlock()

	_, exists := q.activeDownloads[downloadID]
	if !exists {
		logger.Warnf("Download %s not found in active downloads map", downloadID)
	}

	delete(q.activeDownloads, downloadID)
	logger.Debugf("Removed download %s from active downloads", downloadID)

	activeCount := len(q.activeDownloads)
	queuedCount := len(q.queuedDownloads)
	logger.Debugf("Queue status after completion: active=%d, queued=%d, maxConcurrent=%d",
		activeCount, queuedCount, q.maxConcurrent)

	q.fillAvailableSlots()
}

// sortQueue sorts the queue by priority (higher first)
func (q *QueueProcessor) sortQueue() {
	logger.Debugf("Sorting queue with %d downloads by priority", len(q.queuedDownloads))

	sort.Slice(q.queuedDownloads, func(i, j int) bool {
		return q.queuedDownloads[i].Priority > q.queuedDownloads[j].Priority
	})

	if len(q.queuedDownloads) > 0 {
		// Log the highest priority downloads for debugging
		maxToLog := min(3, len(q.queuedDownloads))
		for i := 0; i < maxToLog; i++ {
			logger.Debugf("Queue position %d: download_id=%s, priority=%d", i+1, q.queuedDownloads[i], q.queuedDownloads[i].Priority)
		}
	}
}

// fillAvailableSlots starts downloads if slots are available
func (q *QueueProcessor) fillAvailableSlots() {
	available := q.maxConcurrent - len(q.activeDownloads)
	logger.Debugf("Checking for available slots: active=%d, max=%d, available=%d, queued=%d",
		len(q.activeDownloads), q.maxConcurrent, available, len(q.queuedDownloads))

	if available <= 0 || len(q.queuedDownloads) == 0 {
		if available <= 0 {
			logger.Debugf("No available slots")
		}
		if len(q.queuedDownloads) == 0 {
			logger.Debugf("No downloads in queue")
		}
		return
	}

	toStart := min(available, len(q.queuedDownloads))
	logger.Debugf("Starting %d download(s) to fill available slots", toStart)

	for i := 0; i < toStart; i++ {
		pd := q.queuedDownloads[0]
		priority := pd.Priority

		logger.Debugf("Dequeuing download %s (priority %d) for start", pd.Id, priority)
		q.queuedDownloads = q.queuedDownloads[1:]
		q.activeDownloads[pd.Id] = struct{}{}
		logger.Debugf("Added download %s to active downloads map", pd.Id)

		go func() {
			logger.Debugf("Starting download %s", pd.Id)
			err := q.startDownloadFn(pd.Id)
			if err != nil {
				logger.Errorf("Download %s failed to start: %v", pd.Id, err)
				q.NotifyDownloadCompletion(pd.Id)
			}
		}()
	}

	logger.Debugf("After filling slots: active=%d, queued=%d", len(q.activeDownloads), len(q.queuedDownloads))
}
