package engine

import (
	"container/heap"
	"sync"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/logger"
)

// DownloadItem wraps a download ID with its priority for the heap.
type DownloadItem struct {
	ID       uuid.UUID
	Priority int
	index    int // heap index
}

// downloadHeap implements heap.Interface as a max-heap by Priority.
type downloadHeap []*DownloadItem

func (h downloadHeap) Len() int           { return len(h) }
func (h downloadHeap) Less(i, j int) bool { return h[i].Priority > h[j].Priority }
func (h downloadHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *downloadHeap) Push(x any) {
	item := x.(*DownloadItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *downloadHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	item.index = -1
	*h = old[:n-1]
	return item
}

// QueueProcessor manages prioritized downloads up to maxConcurrent.
type QueueProcessor struct {
	mu            sync.Mutex
	cond          *sync.Cond
	heap          downloadHeap
	startFn       func(uuid.UUID) error
	maxConcurrent int
	activeCount   int
	active        map[uuid.UUID]struct{}
}

// NewQueueProcessor creates and starts the processor loop.
func NewQueueProcessor(maxConcurrent int, startFn func(uuid.UUID) error) *QueueProcessor {
	qp := &QueueProcessor{
		heap:          make(downloadHeap, 0),
		startFn:       startFn,
		maxConcurrent: maxConcurrent,
		active:        make(map[uuid.UUID]struct{}),
	}
	qp.cond = sync.NewCond(&qp.mu)

	heap.Init(&qp.heap)

	go qp.dispatchLoop()
	return qp
}

// Enqueue adds a download ID with its priority into the queue.
func (q *QueueProcessor) Enqueue(id uuid.UUID, priority int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.active[id]; exists {
		logger.Infof("Download %s already active, not enqueuing", id)
		return
	}

	item := &DownloadItem{ID: id, Priority: priority}
	heap.Push(&q.heap, item)
	logger.Infof("Enqueued download %s (priority %d)", id, priority)
	q.cond.Signal()
}

// dispatchLoop pops items when slots free up and starts the workers.
func (q *QueueProcessor) dispatchLoop() {
	for {
		q.mu.Lock()
		for q.activeCount >= q.maxConcurrent || len(q.heap) == 0 {
			q.cond.Wait()
		}

		item := heap.Pop(&q.heap).(*DownloadItem)

		q.activeCount++
		q.active[item.ID] = struct{}{}

		id := item.ID
		q.mu.Unlock()

		go func(downloadID uuid.UUID) {
			logger.Infof("Starting download %s", downloadID)
			err := q.startFn(downloadID)
			if err != nil {
				logger.Errorf("Failed to start download %s: %v", downloadID, err)
			}

			q.mu.Lock()
			q.activeCount--
			delete(q.active, downloadID)
			q.cond.Signal()
			q.mu.Unlock()
		}(id)
	}
}
