package engine

import (
	"container/heap"
	"sync"

	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/google/uuid"
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
}

// NewQueueProcessor creates and starts the processor loop.
func NewQueueProcessor(maxConcurrent int, startFn func(uuid.UUID) error) *QueueProcessor {
	qp := &QueueProcessor{
		heap:          make(downloadHeap, 0),
		startFn:       startFn,
		maxConcurrent: maxConcurrent,
	}
	qp.cond = sync.NewCond(&qp.mu)
	// start the dispatch loop
	go qp.dispatchLoop()
	return qp
}

// Enqueue adds a download ID with its priority into the queue.
func (q *QueueProcessor) Enqueue(id uuid.UUID, priority int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	heap.Push(&q.heap, &DownloadItem{ID: id, Priority: priority})
	logger.Infof("Enqueued download %s (priority %d)", id, priority)
	q.cond.Signal()
}

// dispatchLoop pops items when slots free and starts workers.
func (q *QueueProcessor) dispatchLoop() {
	for {
		q.mu.Lock()
		// wait until we can start someone
		for q.activeCount >= q.maxConcurrent || len(q.heap) == 0 {
			q.cond.Wait()
		}
		// pop highest priority
		item := heap.Pop(&q.heap).(*DownloadItem)
		q.activeCount++
		q.mu.Unlock()

		// run download in its own goroutine
		go func(id uuid.UUID) {
			// when done, release slot and signal
			defer func() {
				q.mu.Lock()
				q.activeCount--
				q.cond.Signal()
				q.mu.Unlock()
			}()

			logger.Infof("Starting download %s", id)
			err := q.startFn(id)
			if err != nil {
				logger.Errorf("Failed to start download %s: %v", id, err)
			}
		}(item.ID)
	}
}
