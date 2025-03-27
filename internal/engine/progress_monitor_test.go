package engine_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/google/uuid"
)

func fixedUUID(s string) uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func fixedTime() time.Time {
	return time.Unix(0, 0)
}

func TestProgressMonitorBroadcast(t *testing.T) {
	progressCh := make(chan common.Progress, 10)
	pm := engine.NewProgressMonitor(progressCh)

	listener := make(chan common.Progress, 1)
	pm.RegisterListener("listener1", listener)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pm.Start(ctx)

	update := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-000000000001"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-000000000002"),
		BytesCompleted: 100,
		TotalBytes:     1000,
		Speed:          50,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	progressCh <- update

	select {
	case p := <-listener:
		if !reflect.DeepEqual(p, update) {
			t.Errorf("expected update %v, got %v", update, p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for progress update")
	}

	update2 := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-000000000003"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-000000000004"),
		BytesCompleted: 200,
		TotalBytes:     2000,
		Speed:          100,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	progressCh <- update2
	select {
	case p := <-listener:
		if !reflect.DeepEqual(p, update2) {
			t.Errorf("expected update %v, got %v", update2, p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for second progress update")
	}

	pm.Stop()
	_, ok := <-listener
	if ok {
		t.Error("expected listener channel to be closed after Stop")
	}
}

func TestProgressMonitorMultipleListeners(t *testing.T) {
	progressCh := make(chan common.Progress, 10)
	pm := engine.NewProgressMonitor(progressCh)

	listener1 := make(chan common.Progress, 1)
	listener2 := make(chan common.Progress, 1)
	pm.RegisterListener("l1", listener1)
	pm.RegisterListener("l2", listener2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pm.Start(ctx)

	update := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-000000000005"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-000000000006"),
		BytesCompleted: 300,
		TotalBytes:     3000,
		Speed:          150,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	progressCh <- update

	select {
	case p := <-listener1:
		if !reflect.DeepEqual(p, update) {
			t.Errorf("listener1: expected %v, got %v", update, p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for listener1 update")
	}
	select {
	case p := <-listener2:
		if !reflect.DeepEqual(p, update) {
			t.Errorf("listener2: expected %v, got %v", update, p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for listener2 update")
	}

	pm.Stop()
}

func TestProgressMonitorContextCancellation(t *testing.T) {
	progressCh := make(chan common.Progress, 10)
	pm := engine.NewProgressMonitor(progressCh)

	listener := make(chan common.Progress, 1)
	pm.RegisterListener("listener1", listener)

	ctx, cancel := context.WithCancel(context.Background())
	pm.Start(ctx)

	updateBefore := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-000000000007"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-000000000008"),
		BytesCompleted: 400,
		TotalBytes:     4000,
		Speed:          200,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	progressCh <- updateBefore

	select {
	case p := <-listener:
		if !reflect.DeepEqual(p, updateBefore) {
			t.Errorf("expected %v, got %v", updateBefore, p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for update before cancel")
	}

	cancel()

	updateAfter := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-000000000009"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-00000000000a"),
		BytesCompleted: 500,
		TotalBytes:     5000,
		Speed:          250,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	progressCh <- updateAfter

	select {
	case p, ok := <-listener:
		if ok {
			t.Errorf("expected no update after context cancellation, got %v", p)
		}
	case <-time.After(100 * time.Millisecond):
		// Expected.
	}
	pm.Stop()
}

func TestProgressMonitorBroadcastNonBlocking(t *testing.T) {
	progressCh := make(chan common.Progress, 10)
	pm := engine.NewProgressMonitor(progressCh)
	// Create a listener channel with capacity 1.
	listener := make(chan common.Progress, 1)
	pm.RegisterListener("listener1", listener)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pm.Start(ctx)

	initial := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-00000000000b"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-00000000000c"),
		BytesCompleted: 600,
		TotalBytes:     6000,
		Speed:          300,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	listener <- initial

	newUpdate := common.Progress{
		DownloadID:     fixedUUID("00000000-0000-0000-0000-00000000000d"),
		ChunkID:        fixedUUID("00000000-0000-0000-0000-00000000000e"),
		BytesCompleted: 700,
		TotalBytes:     7000,
		Speed:          350,
		Status:         common.StatusQueued,
		Error:          nil,
		Timestamp:      fixedTime(),
	}
	progressCh <- newUpdate
	time.Sleep(100 * time.Millisecond)

	select {
	case p := <-listener:
		if !reflect.DeepEqual(p, initial) {
			t.Errorf("expected initial value to remain, got %v", p)
		}
	default:
		t.Error("expected to receive a value from listener")
	}

	pm.Stop()
}
