package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/google/uuid"
)

func TestNoStartWhenZeroConcurrent(t *testing.T) {
	startCh := make(chan uuid.UUID, 10)
	startFn := func(ctx context.Context, d *downloader.Download) error {
		startCh <- d.ID
		<-ctx.Done()
		return ctx.Err()
	}

	qp := engine.NewQueueProcessor(0, startFn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qp.Start(ctx)

	d := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d, 5)

	select {
	case id := <-startCh:
		t.Fatalf("unexpected start call for download %v with maxConcurrent 0", id)
	case <-time.After(200 * time.Millisecond):
		// Expected: no start event.
	}
	qp.Stop()
}

func TestStartUpToConcurrent(t *testing.T) {
	startCh := make(chan uuid.UUID, 10)
	releaseCh := make(chan struct{})

	startFn := func(ctx context.Context, d *downloader.Download) error {
		startCh <- d.ID
		<-releaseCh
		return nil
	}

	qp := engine.NewQueueProcessor(2, startFn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qp.Start(ctx)

	d1 := &downloader.Download{ID: uuid.New()}
	d2 := &downloader.Download{ID: uuid.New()}
	d3 := &downloader.Download{ID: uuid.New()}

	qp.EnqueueDownload(d1, 1)
	qp.EnqueueDownload(d2, 3)
	qp.EnqueueDownload(d3, 2)

	startedIDs := make(map[uuid.UUID]struct{})
	timeout := time.After(500 * time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case id := <-startCh:
			startedIDs[id] = struct{}{}
		case <-timeout:
			t.Fatal("timeout waiting for active downloads to start")
		}
	}
	expectedSet := map[uuid.UUID]struct{}{
		d1.ID: {},
		d2.ID: {},
	}
	if len(startedIDs) != len(expectedSet) {
		t.Errorf("expected %d downloads to start, got %d", len(expectedSet), len(startedIDs))
	}
	for id := range expectedSet {
		if _, ok := startedIDs[id]; !ok {
			t.Errorf("expected download with ID %v to start, but it did not", id)
		}
	}
	qp.NotifyDownloadCompletion(d1.ID)

	var newStart uuid.UUID
	select {
	case newStart = <-startCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for queued download to start")
	}
	if newStart != d3.ID {
		t.Errorf("expected queued download with ID %v to start, got %v", d3.ID, newStart)
	}

	close(releaseCh)
	time.Sleep(100 * time.Millisecond)
	qp.Stop()
}

func TestErrorHandling(t *testing.T) {
	calledCh := make(chan uuid.UUID, 10)
	startFn := func(ctx context.Context, d *downloader.Download) error {
		calledCh <- d.ID
		return errors.New("simulated error")
	}

	qp := engine.NewQueueProcessor(1, startFn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qp.Start(ctx)
	defer qp.Stop()

	d1 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d1, 1)
	select {
	case id := <-calledCh:
		if id != d1.ID {
			t.Errorf("expected d1 to be started, got %v", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("start function was not called for d1")
	}

	d2 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d2, 2)
	select {
	case id := <-calledCh:
		if id != d2.ID {
			t.Errorf("expected d2 to be started next, got %v", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("start function was not called for d2")
	}
}

func TestStopPreventsNewStarts(t *testing.T) {
	startCh := make(chan uuid.UUID, 10)
	startFn := func(ctx context.Context, d *downloader.Download) error {
		startCh <- d.ID
		<-ctx.Done()
		return ctx.Err()
	}

	qp := engine.NewQueueProcessor(1, startFn)
	ctx, cancel := context.WithCancel(context.Background())
	qp.Start(ctx)

	d1 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d1, 1)
	select {
	case <-startCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected first download to start")
	}

	qp.Stop()

	d2 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d2, 2)
	select {
	case id := <-startCh:
		t.Fatalf("unexpected start call for download %v after Stop", id)
	case <-time.After(200 * time.Millisecond):
		// Expected: no start event.
	}
	cancel()
}

func TestContextCancellation(t *testing.T) {
	startCh := make(chan uuid.UUID, 10)
	startFn := func(ctx context.Context, d *downloader.Download) error {
		startCh <- d.ID
		<-ctx.Done()
		return ctx.Err()
	}

	qp := engine.NewQueueProcessor(1, startFn)
	ctx, cancel := context.WithCancel(context.Background())
	qp.Start(ctx)

	d := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d, 1)
	select {
	case <-startCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected download to start")
	}

	cancel()

	d2 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d2, 2)
	select {
	case id := <-startCh:
		t.Fatalf("unexpected start call for download %v after context cancellation", id)
	case <-time.After(200 * time.Millisecond):
		// Expected: no start event.
	}
	qp.Stop()
}

func TestPrioritySorting(t *testing.T) {
	startCh := make(chan uuid.UUID, 10)
	releaseCh := make(chan struct{})

	startFn := func(ctx context.Context, d *downloader.Download) error {
		startCh <- d.ID
		<-releaseCh
		return nil
	}

	qp := engine.NewQueueProcessor(2, startFn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qp.Start(ctx)

	d1 := &downloader.Download{ID: uuid.New()}
	d2 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d1, 1)
	qp.EnqueueDownload(d2, 3)

	startedSet := make(map[uuid.UUID]struct{})
	for i := 0; i < 2; i++ {
		select {
		case id := <-startCh:
			startedSet[id] = struct{}{}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timeout waiting for downloads to start")
		}
	}
	expectedSet := map[uuid.UUID]struct{}{
		d1.ID: {},
		d2.ID: {},
	}
	if len(startedSet) != len(expectedSet) {
		t.Errorf("expected %d downloads to start, got %d", len(expectedSet), len(startedSet))
	}
	for id := range expectedSet {
		if _, ok := startedSet[id]; !ok {
			t.Errorf("expected download with ID %v to have started, but it did not", id)
		}
	}

	d3 := &downloader.Download{ID: uuid.New()}
	qp.EnqueueDownload(d3, 2)
	qp.NotifyDownloadCompletion(d1.ID)
	var newStart uuid.UUID
	select {
	case newStart = <-startCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for queued download to start")
	}
	if newStart != d3.ID {
		t.Errorf("expected queued download with ID %v to start, got %v", d3.ID, newStart)
	}
	close(releaseCh)
	time.Sleep(100 * time.Millisecond)
	qp.Stop()
}
