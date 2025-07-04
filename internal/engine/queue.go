package engine

import (
	"container/heap"
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/worker"
)

// queueItem wraps a Worker so it can live inside a heap.
type queueItem struct {
	worker  worker.Worker
	addedAt time.Time
	index   int
}

type PriorityQueue struct {
	mu            sync.RWMutex
	items         []*queueItem       // the heap
	lookup        map[uuid.UUID]int  // workerID → heap index (or -1 if gone)
	active        map[uuid.UUID]bool // workers currently started
	maxConcurrent int
}

// NewPriorityQueue returns an empty queue that will allow at most
func NewPriorityQueue(maxConcurrent int) *PriorityQueue {
	pq := &PriorityQueue{
		items:         make([]*queueItem, 0),
		lookup:        make(map[uuid.UUID]int),
		active:        make(map[uuid.UUID]bool),
		maxConcurrent: maxConcurrent,
	}
	heap.Init(pq)
	return pq
}

func (pq *PriorityQueue) Len() int { return len(pq.items) }

func (pq *PriorityQueue) Less(i, j int) bool {
	pi, pj := pq.items[i].worker.GetPriority(), pq.items[j].worker.GetPriority()
	if pi != pj {
		return pi > pj
	}

	return pq.items[i].addedAt.Before(pq.items[j].addedAt)
}

func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
	pq.lookup[pq.items[i].worker.GetID()] = i
	pq.lookup[pq.items[j].worker.GetID()] = j
}

func (pq *PriorityQueue) Push(x any) {
	item := x.(*queueItem)
	item.index = len(pq.items)
	pq.items = append(pq.items, item)
	pq.lookup[item.worker.GetID()] = item.index
}

func (pq *PriorityQueue) Pop() any {
	n := len(pq.items) - 1
	item := pq.items[n]
	pq.items[n] = nil
	pq.items = pq.items[:n]
	delete(pq.lookup, item.worker.GetID())
	item.index = -1
	return item
}

func (pq *PriorityQueue) Add(ctx context.Context, w worker.Worker) {
	pq.mu.Lock()
	if _, already := pq.lookup[w.GetID()]; already {
		pq.mu.Unlock()
		return
	}
	heap.Push(pq, &queueItem{worker: w, addedAt: time.Now()})
	pq.mu.Unlock()

	w.Queue()

	pq.process(ctx)
}

func (pq *PriorityQueue) Remove(ctx context.Context, w worker.Worker) {
	pq.mu.Lock()
	idx, ok := pq.lookup[w.GetID()]
	if !ok {
		pq.mu.Unlock()
		return
	}
	heap.Remove(pq, idx)

	delete(pq.active, w.GetID())

	if idx, ok := pq.lookup[w.GetID()]; ok {
		heap.Remove(pq, idx)
		delete(pq.lookup, w.GetID())
	}
	pq.mu.Unlock()

	pq.process(ctx)
}

func (pq *PriorityQueue) process(ctx context.Context) {
	var toStart []worker.Worker
	var toPause []worker.Worker

	pq.mu.Lock()

	itemsCopy := make([]*queueItem, len(pq.items))
	copy(itemsCopy, pq.items)
	sort.Slice(itemsCopy, func(i, j int) bool { // same rule as Less()
		pi, pj := itemsCopy[i].worker.GetPriority(), itemsCopy[j].worker.GetPriority()
		if pi != pj {
			return pi > pj
		}
		return itemsCopy[i].addedAt.Before(itemsCopy[j].addedAt)
	})
	if len(itemsCopy) > pq.maxConcurrent {
		itemsCopy = itemsCopy[:pq.maxConcurrent]
	}

	newActive := make(map[uuid.UUID]bool, pq.maxConcurrent)
	for _, item := range itemsCopy {
		id := item.worker.GetID()
		newActive[id] = true
		if !pq.active[id] {
			toStart = append(toStart, item.worker)
		}
	}

	for id := range pq.active {
		if !newActive[id] {
			if idx, ok := pq.lookup[id]; ok && idx >= 0 && idx < len(pq.items) {
				toPause = append(toPause, pq.items[idx].worker)
			}
		}
	}

	pq.active = newActive

	pq.mu.Unlock()

	for _, w := range toStart {
		_ = w.Start(ctx)
	}

	for _, w := range toPause {
		_ = w.Pause()
		w.Queue()
	}
}
