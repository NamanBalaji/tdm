package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestQueueProcessor_Basic ensures the queue processes items in priority order
func TestQueueProcessor_PriorityAndBlocking(t *testing.T) {
	// Use channels to control and observe execution flow
	startCh := make(chan uuid.UUID, 10)

	// Signal when a download can complete
	var doneMu sync.Mutex
	completeSignals := make(map[uuid.UUID]chan struct{})

	// StartFn just signals that it started and waits for completion signal
	startFn := func(id uuid.UUID) error {
		startCh <- id

		// Create a channel to wait on before this function returns
		doneMu.Lock()
		done := make(chan struct{})
		completeSignals[id] = done
		doneMu.Unlock()

		<-done
		return nil
	}

	// Create processor with max 1 concurrent download
	qp := NewQueueProcessor(1, startFn)

	// First, enqueue a low priority download
	idLow := uuid.New()
	qp.Enqueue(idLow, 1)

	// Wait for it to start
	var firstStarted uuid.UUID
	select {
	case firstStarted = <-startCh:
		if firstStarted != idLow {
			t.Fatalf("Expected first download to be %v, got %v", idLow, firstStarted)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("First download didn't start in time")
	}

	// Add a high priority download - it should be queued until first completes
	idHigh := uuid.New()
	qp.Enqueue(idHigh, 2)

	// Verify the second download doesn't start yet (no slot available)
	select {
	case unexpected := <-startCh:
		t.Fatalf("Unexpected download started before first completed: %v", unexpected)
	case <-time.After(50 * time.Millisecond):
		// This is expected - no second download should start yet
	}

	// Complete the first download
	doneMu.Lock()
	close(completeSignals[firstStarted])
	delete(completeSignals, firstStarted)
	doneMu.Unlock()

	// Now the high priority download should start
	select {
	case secondStarted := <-startCh:
		if secondStarted != idHigh {
			t.Fatalf("Expected high priority download %v to start, got %v", idHigh, secondStarted)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Second download didn't start after first completed")
	}

	// Clean up
	doneMu.Lock()
	for id, ch := range completeSignals {
		close(ch)
		delete(completeSignals, id)
	}
	doneMu.Unlock()
}

// TestQueueProcessor_MultipleConcurrent tests multiple concurrent downloads
func TestQueueProcessor_MultipleConcurrent(t *testing.T) {
	// Setup
	startCh := make(chan uuid.UUID, 10)
	var doneMu sync.Mutex
	completeSignals := make(map[uuid.UUID]chan struct{})

	startFn := func(id uuid.UUID) error {
		startCh <- id

		doneMu.Lock()
		done := make(chan struct{})
		completeSignals[id] = done
		doneMu.Unlock()

		<-done
		return nil
	}

	// Create processor with max 2 concurrent downloads
	qp := NewQueueProcessor(2, startFn)

	// Create three downloads with different priorities
	idLow := uuid.New()
	idMed := uuid.New()
	idHigh := uuid.New()

	t.Logf("Low priority ID (1): %v", idLow)
	t.Logf("Medium priority ID (2): %v", idMed)
	t.Logf("High priority ID (3): %v", idHigh)

	// Add all three downloads in sequence (explicitly not in priority order)
	qp.Enqueue(idLow, 1)
	qp.Enqueue(idMed, 2)
	qp.Enqueue(idHigh, 3)

	// The top two priorities should start (med and high)
	started := make(map[uuid.UUID]bool)
	for i := 0; i < 2; i++ {
		select {
		case id := <-startCh:
			started[id] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Didn't get %d started downloads in time", 2)
		}
	}

	// Verify which ones started
	if !started[idMed] || !started[idHigh] {
		t.Logf("Started downloads: %v", started)
		t.Errorf("Expected medium and high priority downloads to start")
	}

	// Verify no other downloads start yet
	select {
	case id := <-startCh:
		t.Fatalf("Unexpected third download started: %v", id)
	case <-time.After(50 * time.Millisecond):
		// This is expected
	}

	// Complete the high priority download
	doneMu.Lock()
	if ch, ok := completeSignals[idHigh]; ok {
		close(ch)
		delete(completeSignals, idHigh)
	}
	doneMu.Unlock()

	// Now the low priority should start
	select {
	case id := <-startCh:
		if id != idLow {
			t.Fatalf("Expected low priority download to start, got %v", id)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Low priority download didn't start after slot freed")
	}

	// Clean up
	doneMu.Lock()
	for id, ch := range completeSignals {
		close(ch)
		delete(completeSignals, id)
	}
	doneMu.Unlock()
}
