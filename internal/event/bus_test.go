package event

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelDebug,
}))

func TestEventBusBasic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	bus := NewEventBus(logger, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := bus.Subscribe(ctx, TaskCreated)

	bus.Emit(TaskCreated, "task-123")

	select {
	case e := <-sub:
		id, ok := e.Data.(string)
		assert.True(t, ok)
		assert.Equal(t, "task-123", id)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestContextCancellation(t *testing.T) {
	bus := NewEventBus(logger, 0)
	ctx, cancel := context.WithCancel(context.Background())
	sub := bus.Subscribe(ctx, TaskCreated)

	cancel()

	_, ok := <-sub
	assert.False(t, ok)
}

func TestConcurrentOperations(t *testing.T) {
	bus := NewEventBus(logger, 100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		emitterWG    sync.WaitGroup
		subscriberWG sync.WaitGroup
	)

	// Start subscribers
	for i := 0; i < 10; i++ {
		subscriberWG.Add(1)
		go func() {
			defer subscriberWG.Done()
			sub := bus.Subscribe(ctx, TaskCreated)
			for {
				select {
				case _, ok := <-sub:
					if !ok {
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	for i := 0; i < 5; i++ {
		emitterWG.Add(1)
		go func() {
			defer emitterWG.Done()
			for j := 0; j < 100; j++ {
				bus.Emit(TaskCreated, j)
			}
		}()
	}

	emitterWG.Wait()

	cancel()

	subscriberWG.Wait()
}

func TestChannelFull(t *testing.T) {
	var logged bool
	logger := slog.New(slog.NewTextHandler(&testWriter{t: t, fn: func() { logged = true }}, nil))

	bus := NewEventBus(logger, 1)

	ctx := context.Background()
	bus.Subscribe(ctx, TaskCreated)

	bus.Emit(TaskCreated, "1")
	bus.Emit(TaskCreated, "2")

	time.Sleep(100 * time.Millisecond)
	assert.True(t, logged, "should log channel full")
}

type testWriter struct {
	t  *testing.T
	fn func()
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	w.t.Helper()
	w.fn()
	return len(p), nil
}
