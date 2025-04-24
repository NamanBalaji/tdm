package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestQueueProcessor_PriorityAndBlocking(t *testing.T) {
	var mu sync.Mutex
	doneMap := make(map[uuid.UUID]chan struct{})
	startedCh := make(chan uuid.UUID, 2)

	startFn := func(id uuid.UUID) error {
		d := make(chan struct{})
		mu.Lock()
		doneMap[id] = d
		mu.Unlock()
		startedCh <- id
		<-d
		return nil
	}

	doneCh := make(chan struct{})
	qp := NewQueueProcessor(1, startFn, doneCh)
	defer close(doneCh)

	idLow := uuid.New()
	idHigh := uuid.New()

	qp.Enqueue(idLow, 1)
	qp.Enqueue(idHigh, 2)

	select {
	case id := <-startedCh:
		if id != idHigh {
			t.Fatalf("expected high-priority %v first, got %v", idHigh, id)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for high-priority start")
	}

	select {
	case id := <-startedCh:
		t.Fatalf("unexpected start before finish: %v", id)
	case <-time.After(50 * time.Millisecond):
	}

	mu.Lock()
	close(doneMap[idHigh])
	mu.Unlock()

	select {
	case id := <-startedCh:
		if id != idLow {
			t.Fatalf("expected low-priority %v next, got %v", idLow, id)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for low-priority start")
	}
}

func TestQueueProcessor_MultipleConcurrent(t *testing.T) {
	var mu sync.Mutex
	doneMap := make(map[uuid.UUID]chan struct{})
	startedCh := make(chan uuid.UUID, 3)

	startFn := func(id uuid.UUID) error {
		d := make(chan struct{})
		mu.Lock()
		doneMap[id] = d
		mu.Unlock()
		startedCh <- id
		<-d
		return nil
	}

	doneCh := make(chan struct{})
	qp := NewQueueProcessor(2, startFn, doneCh)
	defer close(doneCh)

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	prios := []int{1, 2, 3}
	for i, id := range ids {
		qp.Enqueue(id, prios[i])
	}

	var first, second uuid.UUID
	select {
	case first = <-startedCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for first start")
	}
	select {
	case second = <-startedCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for second start")
	}
	set := map[uuid.UUID]bool{ids[2]: true, ids[1]: true}
	if !set[first] || !set[second] {
		t.Errorf("expected first two to be %v and %v, got %v and %v", ids[2], ids[1], first, second)
	}

	select {
	case id := <-startedCh:
		t.Fatalf("unexpected third start before slot freed: %v", id)
	case <-time.After(50 * time.Millisecond):
	}

	mu.Lock()
	close(doneMap[first])
	mu.Unlock()

	select {
	case id := <-startedCh:
		if id != ids[0] {
			t.Errorf("expected third %v, got %v", ids[0], id)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for third start")
	}

	mu.Lock()
	close(doneMap[second])
	close(doneMap[ids[0]])
	mu.Unlock()
}
