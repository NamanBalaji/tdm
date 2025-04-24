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
	index    int
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

// QueueProcessor manages prioritized downloads up to maxConcurrent,
// and will exit its dispatchLoop when stopCh is closed.
type QueueProcessor struct {
	mu            sync.Mutex
	cond          *sync.Cond
	heap          downloadHeap
	startFn       func(uuid.UUID) error
	maxConcurrent int
	activeCount   int
	stopCh        <-chan struct{}
}

// NewQueueProcessor creates and starts the processor loop.
// When stopCh is closed, dispatchLoop will wake up and return.
func NewQueueProcessor(maxConcurrent int, startFn func(uuid.UUID) error, stopCh <-chan struct{}) *QueueProcessor {
	qp := &QueueProcessor{
		heap:          make(downloadHeap, 0),
		startFn:       startFn,
		maxConcurrent: maxConcurrent,
		stopCh:        stopCh,
	}
	qp.cond = sync.NewCond(&qp.mu)

	// Kick off the dispatch goroutine
	go qp.dispatchLoop()

	// Also watch stopCh so we can wake any waiting cond.Wait()
	go func() {
		<-stopCh
		qp.cond.L.Lock()
		qp.cond.Broadcast()
		qp.cond.L.Unlock()
	}()

	return qp
}

// Enqueue adds a download ID with its priority into the queue.
func (q *QueueProcessor) Enqueue(id uuid.UUID, priority int) {
	q.mu.Lock()
	heap.Push(&q.heap, &DownloadItem{ID: id, Priority: priority})
	logger.Infof("Enqueued download %s (priority %d)", id, priority)
	q.cond.Signal()
	q.mu.Unlock()
}

// dispatchLoop pops items when slots free and starts workers.
// It will return as soon as stopCh is closed.
func (q *QueueProcessor) dispatchLoop() {
	for {
		q.mu.Lock()
		// Wait until: a slot is free AND there's work OR we’ve been asked to stop.
		for q.activeCount >= q.maxConcurrent || len(q.heap) == 0 {
			q.cond.Wait()
			// After waking, check for shutdown.
			select {
			case <-q.stopCh:
				q.mu.Unlock()
				return
			default:
			}
		}

		select {
		case <-q.stopCh:
			q.mu.Unlock()
			return
		default:
		}

		// Pop highest-priority item and consume a slot
		item := heap.Pop(&q.heap).(*DownloadItem)
		q.activeCount++
		q.mu.Unlock()

		// Launch the download; when done, free the slot and signal.
		go func(id uuid.UUID) {
			defer func() {
				q.mu.Lock()
				q.activeCount--
				q.cond.Signal()
				q.mu.Unlock()
			}()

			logger.Infof("Starting download %s", id)
			if err := q.startFn(id); err != nil {
				logger.Errorf("Failed to start download %s: %v", id, err)
			}
		}(item.ID)
	}
}
