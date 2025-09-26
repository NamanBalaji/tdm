package engine_test

import (
	"context"
	"sync"
	"testing"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/google/uuid"
)

// mockWorker implements the worker.Worker interface for testing
type mockWorker struct {
	id          uuid.UUID
	priority    int
	startCalled bool
	pauseCalled bool
	queueCalled bool
	isActive    bool // tracks current state
	mu          sync.Mutex
}

func newMockWorker(priority int) *mockWorker {
	return &mockWorker{
		id:       uuid.New(),
		priority: priority,
	}
}

func (m *mockWorker) GetID() uuid.UUID {
	return m.id
}

func (m *mockWorker) GetPriority() int {
	return m.priority
}

func (m *mockWorker) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = true
	m.isActive = true
	return nil
}

func (m *mockWorker) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauseCalled = true
	m.isActive = false
	return nil
}

func (m *mockWorker) Queue() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queueCalled = true
}

// Stub implementations for other interface methods
func (m *mockWorker) Cancel() error                  { return nil }
func (m *mockWorker) Resume(_ context.Context) error { return nil }
func (m *mockWorker) Remove() error                  { return nil }
func (m *mockWorker) Done() <-chan error             { return make(chan error) }
func (m *mockWorker) Progress() progress.Progress    { return progress.Progress{} }
func (m *mockWorker) GetStatus() status.Status       { return 0 }
func (m *mockWorker) GetFilename() string            { return "" }

// Helper methods for testing
func (m *mockWorker) wasStartCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *mockWorker) wasPauseCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pauseCalled
}

func (m *mockWorker) wasQueueCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queueCalled
}

func (m *mockWorker) isCurrentlyActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isActive
}

func (m *mockWorker) resetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = false
	m.pauseCalled = false
	m.queueCalled = false
	m.isActive = false
}

func TestNewPriorityQueue(t *testing.T) {
	tests := []struct {
		name          string
		maxConcurrent int
		wantLen       int
	}{
		{"zero concurrent", 0, 0},
		{"one concurrent", 1, 0},
		{"multiple concurrent", 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pq := engine.NewPriorityQueue(tt.maxConcurrent)

			if got := pq.Len(); got != tt.wantLen {
				t.Errorf("NewPriorityQueue().Len() = %v, want %v", got, tt.wantLen)
			}
		})
	}
}

func TestPriorityQueue_Add(t *testing.T) {
	tests := []struct {
		name          string
		maxConcurrent int
		workers       []int // priorities
		expectActive  []int // indices of workers that should be active
	}{
		{
			name:          "single worker under limit",
			maxConcurrent: 2,
			workers:       []int{5},
			expectActive:  []int{0},
		},
		{
			name:          "multiple workers under limit",
			maxConcurrent: 3,
			workers:       []int{5, 3},
			expectActive:  []int{0, 1},
		},
		{
			name:          "workers over limit - priority order",
			maxConcurrent: 1,
			workers:       []int{5, 8, 3},
			expectActive:  []int{1}, // worker with priority 8
		},
		{
			name:          "workers over limit - multiple high priority",
			maxConcurrent: 2,
			workers:       []int{5, 8, 9, 3},
			expectActive:  []int{2, 1}, // workers with priority 9 and 8
		},
		{
			name:          "zero concurrent",
			maxConcurrent: 0,
			workers:       []int{5, 3},
			expectActive:  []int{}, // no workers should be active
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pq := engine.NewPriorityQueue(tt.maxConcurrent)
			ctx := context.Background()

			// Create workers
			workers := make([]*mockWorker, len(tt.workers))
			for i, priority := range tt.workers {
				workers[i] = newMockWorker(priority)
			}

			// Add all workers
			for _, w := range workers {
				pq.Add(ctx, w)
			}

			// Check queue length
			if got := pq.Len(); got != len(workers) {
				t.Errorf("queue length = %v, want %v", got, len(workers))
			}

			// Check which workers are currently active
			for i, w := range workers {
				expectedActive := false
				for _, idx := range tt.expectActive {
					if i == idx {
						expectedActive = true
						break
					}
				}

				if w.isCurrentlyActive() != expectedActive {
					t.Errorf("worker %d (priority %d): expected active=%v, got active=%v",
						i, w.priority, expectedActive, w.isCurrentlyActive())
				}

				// All workers should have Queue() called
				if !w.wasQueueCalled() {
					t.Errorf("worker %d: Queue() was not called", i)
				}
			}
		})
	}
}

func TestPriorityQueue_Remove(t *testing.T) {
	tests := []struct {
		name              string
		maxConcurrent     int
		workers           []int // priorities
		removeIndex       int   // index of worker to remove
		expectActiveAfter []int // indices that should be active after remove
	}{
		{
			name:              "remove active worker - replacement starts",
			maxConcurrent:     1,
			workers:           []int{5, 3},
			removeIndex:       0,        // remove higher priority worker
			expectActiveAfter: []int{1}, // lower priority worker should become active
		},
		{
			name:              "remove inactive worker - no change",
			maxConcurrent:     1,
			workers:           []int{5, 3},
			removeIndex:       1,        // remove lower priority worker (not active)
			expectActiveAfter: []int{0}, // high priority worker remains active
		},
		{
			name:              "remove from multiple active",
			maxConcurrent:     2,
			workers:           []int{5, 8, 3},
			removeIndex:       1,           // remove worker with priority 8
			expectActiveAfter: []int{0, 2}, // workers with priority 5,3 should be active
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pq := engine.NewPriorityQueue(tt.maxConcurrent)
			ctx := context.Background()

			// Create workers
			workers := make([]*mockWorker, len(tt.workers))
			for i, priority := range tt.workers {
				workers[i] = newMockWorker(priority)
			}

			// Add all workers
			for _, w := range workers {
				pq.Add(ctx, w)
			}

			initialLen := pq.Len()

			// Remove worker
			pq.Remove(ctx, workers[tt.removeIndex])

			// Check queue length decreased
			if got := pq.Len(); got != initialLen-1 {
				t.Errorf("after remove: queue length = %v, want %v", got, initialLen-1)
			}

			// Check final state - which workers should be active
			for i, w := range workers {
				if i == tt.removeIndex {
					continue // skip removed worker
				}

				expectedActive := false
				for _, idx := range tt.expectActiveAfter {
					if i == idx {
						expectedActive = true
						break
					}
				}

				if w.isCurrentlyActive() != expectedActive {
					t.Errorf("after remove: worker %d (priority %d): expected active=%v, got active=%v",
						i, w.priority, expectedActive, w.isCurrentlyActive())
				}
			}
		})
	}
}

func TestPriorityQueue_AddDuplicate(t *testing.T) {
	pq := engine.NewPriorityQueue(2)
	ctx := context.Background()

	worker := newMockWorker(5)

	// Add worker first time
	pq.Add(ctx, worker)

	if pq.Len() != 1 {
		t.Errorf("after first add: queue length = %v, want 1", pq.Len())
	}

	// Reset flags
	worker.resetCalls()

	// Add same worker again
	pq.Add(ctx, worker)

	// Should not increase length
	if pq.Len() != 1 {
		t.Errorf("after duplicate add: queue length = %v, want 1", pq.Len())
	}

	// Should not call Queue again
	if worker.wasQueueCalled() {
		t.Error("Queue() should not be called for duplicate add")
	}
}

func TestPriorityQueue_RemoveNonExistent(t *testing.T) {
	pq := engine.NewPriorityQueue(2)
	ctx := context.Background()

	worker1 := newMockWorker(5)
	worker2 := newMockWorker(3)

	// Add one worker
	pq.Add(ctx, worker1)

	if pq.Len() != 1 {
		t.Errorf("queue length = %v, want 1", pq.Len())
	}

	// Try to remove worker that was never added
	pq.Remove(ctx, worker2)

	// Length should remain the same
	if pq.Len() != 1 {
		t.Errorf("after remove non-existent: queue length = %v, want 1", pq.Len())
	}
}

func TestPriorityQueue_StartPauseBehavior(t *testing.T) {
	t.Run("worker gets paused when higher priority added", func(t *testing.T) {
		pq := engine.NewPriorityQueue(1)
		ctx := context.Background()

		// Add low priority worker first
		lowPriorityWorker := newMockWorker(3)
		pq.Add(ctx, lowPriorityWorker)

		// Should be active initially
		if !lowPriorityWorker.isCurrentlyActive() {
			t.Error("low priority worker should be active initially")
		}

		// Add high priority worker
		highPriorityWorker := newMockWorker(8)
		pq.Add(ctx, highPriorityWorker)

		// High priority should now be active, low priority should be paused
		if !highPriorityWorker.isCurrentlyActive() {
			t.Error("high priority worker should be active")
		}

		if lowPriorityWorker.isCurrentlyActive() {
			t.Error("low priority worker should be paused")
		}

		// Check that methods were called appropriately
		if !lowPriorityWorker.wasStartCalled() {
			t.Error("low priority worker should have been started initially")
		}

		if !lowPriorityWorker.wasPauseCalled() {
			t.Error("low priority worker should have been paused")
		}

		if !highPriorityWorker.wasStartCalled() {
			t.Error("high priority worker should have been started")
		}

		if highPriorityWorker.wasPauseCalled() {
			t.Error("high priority worker should not have been paused")
		}
	})
}

func TestPriorityQueue_SamePriorityOrdering(t *testing.T) {
	pq := engine.NewPriorityQueue(1)
	ctx := context.Background()

	// Add workers with same priority in sequence
	worker1 := newMockWorker(5)
	worker2 := newMockWorker(5)
	worker3 := newMockWorker(5)

	pq.Add(ctx, worker1)
	pq.Add(ctx, worker2)
	pq.Add(ctx, worker3)

	// First worker should be active (added earliest)
	if !worker1.isCurrentlyActive() {
		t.Error("first worker (earliest) should be active")
	}

	if worker2.isCurrentlyActive() {
		t.Error("second worker should not be active")
	}

	if worker3.isCurrentlyActive() {
		t.Error("third worker should not be active")
	}

	// Remove first worker
	pq.Remove(ctx, worker1)

	// Second worker should now be active
	if !worker2.isCurrentlyActive() {
		t.Error("second worker should be active after first is removed")
	}

	if worker3.isCurrentlyActive() {
		t.Error("third worker should still not be active")
	}
}

func TestPriorityQueue_EdgeCases(t *testing.T) {
	t.Run("empty queue operations", func(t *testing.T) {
		pq := engine.NewPriorityQueue(1)
		ctx := context.Background()

		// Remove from empty queue should not panic
		worker := newMockWorker(5)
		pq.Remove(ctx, worker)

		if pq.Len() != 0 {
			t.Errorf("empty queue length = %v, want 0", pq.Len())
		}
	})

	t.Run("zero max concurrent", func(t *testing.T) {
		pq := engine.NewPriorityQueue(0)
		ctx := context.Background()

		worker := newMockWorker(5)
		pq.Add(ctx, worker)

		// Worker should be queued but not active
		if worker.isCurrentlyActive() {
			t.Error("worker should not be active when maxConcurrent is 0")
		}

		if !worker.wasQueueCalled() {
			t.Error("worker should still be queued")
		}
	})

	t.Run("negative priority", func(t *testing.T) {
		pq := engine.NewPriorityQueue(2)
		ctx := context.Background()

		worker1 := newMockWorker(-5)
		worker2 := newMockWorker(3)

		pq.Add(ctx, worker1)
		pq.Add(ctx, worker2)

		// Both should be active since we're under the limit
		if !worker2.isCurrentlyActive() {
			t.Error("worker with priority 3 should be active")
		}
		if !worker1.isCurrentlyActive() {
			t.Error("worker with priority -5 should also be active (under limit)")
		}
	})
}

func TestPriorityQueue_HeapInterface(t *testing.T) {
	t.Run("heap ordering", func(t *testing.T) {
		pq := engine.NewPriorityQueue(10) // high limit to test ordering
		ctx := context.Background()

		// Add workers in non-priority order
		priorities := []int{3, 9, 1, 7, 5}
		workers := make([]*mockWorker, len(priorities))

		for i, p := range priorities {
			workers[i] = newMockWorker(p)
			pq.Add(ctx, workers[i])
		}

		// All should be active since we're under the limit
		for i, w := range workers {
			if !w.isCurrentlyActive() {
				t.Errorf("worker %d (priority %d) should be active", i, w.priority)
			}
		}

		// Length should match
		if pq.Len() != len(workers) {
			t.Errorf("queue length = %v, want %v", pq.Len(), len(workers))
		}
	})

	t.Run("complex priority scenarios", func(t *testing.T) {
		pq := engine.NewPriorityQueue(3)
		ctx := context.Background()

		// Create workers with mixed priorities
		workers := []*mockWorker{
			newMockWorker(1),  // 0: low
			newMockWorker(10), // 1: highest
			newMockWorker(5),  // 2: medium
			newMockWorker(8),  // 3: high
			newMockWorker(3),  // 4: low-medium
		}

		// Add them all
		for _, w := range workers {
			pq.Add(ctx, w)
		}

		// Top 3 by priority should be active: workers[1], workers[3], workers[2]
		expectedActive := []int{1, 3, 2} // priorities 10, 8, 5

		for i, w := range workers {
			expected := false
			for _, idx := range expectedActive {
				if i == idx {
					expected = true
					break
				}
			}

			if w.isCurrentlyActive() != expected {
				t.Errorf("worker %d (priority %d): expected active=%v, got active=%v",
					i, w.priority, expected, w.isCurrentlyActive())
			}
		}
	})
}

func TestPriorityQueue_ConcurrentAccess(t *testing.T) {
	pq := engine.NewPriorityQueue(5)
	ctx := context.Background()

	var wg sync.WaitGroup
	numWorkers := 20
	workers := make([]*mockWorker, numWorkers)

	// Create workers
	for i := 0; i < numWorkers; i++ {
		workers[i] = newMockWorker(i % 10) // priorities 0-9
	}

	// Add workers concurrently
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pq.Add(ctx, workers[idx])
		}(i)
	}

	wg.Wait()

	// All workers should be in queue
	if pq.Len() != numWorkers {
		t.Errorf("queue length = %v, want %v", pq.Len(), numWorkers)
	}

	// Remove half of them concurrently
	for i := 0; i < numWorkers/2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pq.Remove(ctx, workers[idx])
		}(i)
	}

	wg.Wait()

	// Should have half the workers left
	expectedLen := numWorkers - numWorkers/2
	if pq.Len() != expectedLen {
		t.Errorf("after concurrent removes: queue length = %v, want %v", pq.Len(), expectedLen)
	}
}
