package engine

import (
	"container/heap"
	"sync"
	"time"

	"github.com/google/uuid"
)

// QueueItem represents an item in the priority queue.
type QueueItem struct {
	ID       uuid.UUID
	Priority int
	AddedAt  time.Time
	index    int
}

// PriorityQueue implements a thread-safe priority queue.
type PriorityQueue struct {
	mu     sync.RWMutex
	items  []*QueueItem
	lookup map[uuid.UUID]*QueueItem
}

// NewPriorityQueue creates a new priority queue.
func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{
		items:  make([]*QueueItem, 0),
		lookup: make(map[uuid.UUID]*QueueItem),
	}
	heap.Init(pq)

	return pq
}

func (pq *PriorityQueue) Len() int { return len(pq.items) }

func (pq *PriorityQueue) Less(i, j int) bool {
	if pq.items[i].Priority != pq.items[j].Priority {
		return pq.items[i].Priority > pq.items[j].Priority
	}

	return pq.items[i].AddedAt.Before(pq.items[j].AddedAt)
}

func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(pq.items)
	item := x.(*QueueItem)
	item.index = n
	pq.items = append(pq.items, item)
	pq.lookup[item.ID] = item
}

func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	pq.items = old[0 : n-1]
	delete(pq.lookup, item.ID)

	return item
}

// Add adds a new item to the queue.
func (pq *PriorityQueue) Add(id uuid.UUID, priority int) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if _, exists := pq.lookup[id]; exists {
		return
	}

	item := &QueueItem{
		ID:       id,
		Priority: priority,
		AddedAt:  time.Now(),
	}
	heap.Push(pq, item)
}

// Remove removes an item from the queue.
func (pq *PriorityQueue) Remove(id uuid.UUID) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	item, exists := pq.lookup[id]
	if !exists {
		return
	}

	heap.Remove(pq, item.index)
}

// Peek returns the highest priority item without removing it.
func (pq *PriorityQueue) Peek() *QueueItem {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	if len(pq.items) == 0 {
		return nil
	}

	item := pq.items[0]

	return &QueueItem{
		ID:       item.ID,
		Priority: item.Priority,
		AddedAt:  item.AddedAt,
	}
}

// PopHighest removes and returns the highest priority item.
func (pq *PriorityQueue) PopHighest() *QueueItem {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.items) == 0 {
		return nil
	}

	return heap.Pop(pq).(*QueueItem)
}

// GetLowestPriority returns the item with lowest priority.
func (pq *PriorityQueue) GetLowestPriority(excludeIDs ...uuid.UUID) *QueueItem {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	if len(pq.items) == 0 {
		return nil
	}

	exclude := make(map[uuid.UUID]bool)
	for _, id := range excludeIDs {
		exclude[id] = true
	}

	var lowest *QueueItem

	for _, item := range pq.items {
		if exclude[item.ID] {
			continue
		}

		if lowest == nil || item.Priority < lowest.Priority ||
			(item.Priority == lowest.Priority && item.AddedAt.After(lowest.AddedAt)) {
			lowest = item
		}
	}

	if lowest == nil {
		return nil
	}

	return &QueueItem{
		ID:       lowest.ID,
		Priority: lowest.Priority,
		AddedAt:  lowest.AddedAt,
	}
}

// Size returns the number of items in the queue.
func (pq *PriorityQueue) Size() int {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	return len(pq.items)
}

// Clear removes all items from the queue.
func (pq *PriorityQueue) Clear() {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.items = make([]*QueueItem, 0)
	pq.lookup = make(map[uuid.UUID]*QueueItem)
	heap.Init(pq)
}

// Contains checks if an item exists in the queue.
func (pq *PriorityQueue) Contains(id uuid.UUID) bool {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	_, exists := pq.lookup[id]

	return exists
}

// GetAll returns all items in priority order.
func (pq *PriorityQueue) GetAll() []*QueueItem {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	result := make([]*QueueItem, len(pq.items))
	for i, item := range pq.items {
		result[i] = &QueueItem{
			ID:       item.ID,
			Priority: item.Priority,
			AddedAt:  item.AddedAt,
		}
	}

	sorted := make([]*QueueItem, len(result))
	copy(sorted, result)
	tempPQ := &PriorityQueue{items: sorted}
	heap.Init(tempPQ)

	for i := 0; i < len(result); i++ {
		result[i] = heap.Pop(tempPQ).(*QueueItem)
	}

	return result
}
