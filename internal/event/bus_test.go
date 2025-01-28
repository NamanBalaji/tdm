package event_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testLogger struct {
	*slog.Logger
	warnings []string
}

func (tl *testLogger) Warn(msg string, args ...any) {
	tl.warnings = append(tl.warnings, msg)
}

func TestEventBus(t *testing.T) {
	t.Run("SingleSubscriberReceivesEvent", func(t *testing.T) {
		logger := &testLogger{
			Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})),
		}
		bus := event.NewEventBus(logger.Logger, 10)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch := bus.Subscribe(ctx, event.TaskCreated)
		expectedData := map[string]string{"task_id": "123"}

		bus.Emit(event.TaskCreated, expectedData)

		select {
		case e := <-ch:
			assert.Equal(t, event.TaskCreated, e.Type)
			assert.Equal(t, expectedData, e.Data)
			assert.False(t, e.Timestamp.IsZero())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Did not receive event within timeout")
		}
	})

	t.Run("MultipleSubscribersSameEventType", func(t *testing.T) {
		logger := &testLogger{
			Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})),
		}

		bus := event.NewEventBus(logger.Logger, 10)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var wg sync.WaitGroup
		const subscribers = 5
		wg.Add(subscribers)

		for i := 0; i < subscribers; i++ {
			ch := bus.Subscribe(ctx, event.ChunkProgress)
			go func() {
				defer wg.Done()
				<-ch // Ensure we receive the event
			}()
		}

		bus.Emit(event.ChunkProgress, map[string]int{"bytes": 1024})
		wg.Wait() // Wait for all subscribers to receive
	})

	t.Run("UnsubscribeViaContextCancellation", func(t *testing.T) {
		logger := &testLogger{
			Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})),
		}

		bus := event.NewEventBus(logger.Logger, 10)
		ctx, cancel := context.WithCancel(context.Background())

		ch := bus.Subscribe(ctx, event.TaskCompleted)
		cancel() // Unsubscribe

		// Wait for unsubscribe to complete
		time.Sleep(50 * time.Millisecond)
		bus.Emit(event.TaskCompleted, nil)

		select {
		case _, ok := <-ch:
			assert.False(t, ok, "Channel should be closed")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Channel not closed after unsubscribe")
		}
	})

	t.Run("DifferentEventTypesDontInterfere", func(t *testing.T) {
		logger := &testLogger{
			Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})),
		}

		bus := event.NewEventBus(logger.Logger, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mainCh := bus.Subscribe(ctx, event.TaskCreated)
		otherCh := bus.Subscribe(ctx, event.TaskCompleted)

		bus.Emit(event.TaskCreated, "create")
		bus.Emit(event.TaskCompleted, "complete")

		select {
		case e := <-mainCh:
			assert.Equal(t, event.TaskCreated, e.Type)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Main subscriber didn't receive event")
		}

		select {
		case e := <-otherCh:
			assert.Equal(t, event.TaskCompleted, e.Type)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Other subscriber didn't receive event")
		}
	})

	t.Run("HighConcurrencyStressTest", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // Disable logs for this test
		bus := event.NewEventBus(logger, 1000)                   // Large buffer to prevent drops
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		const (
			emitters   = 10
			eventsEach = 100
		)

		var (
			emitWg     sync.WaitGroup
			eventCount atomic.Int32
			done       = make(chan struct{})
		)

		// Single subscriber with guaranteed processing
		ch := bus.Subscribe(ctx, event.ChunkProgress)
		go func() {
			for range ch {
				eventCount.Add(1)
			}
			close(done)
		}()

		// Start emitters
		emitWg.Add(emitters)
		for i := 0; i < emitters; i++ {
			go func() {
				defer emitWg.Done()
				for j := 0; j < eventsEach; j++ {
					bus.Emit(event.ChunkProgress, j)
				}
			}()
		}

		// Wait for all emits to complete
		emitWg.Wait()

		// Allow time for remaining events to be processed
		time.Sleep(100 * time.Millisecond)

		// Close and wait for subscriber
		cancel()
		<-done

		expected := emitters * eventsEach
		assert.Equal(t, int32(expected), eventCount.Load(),
			"Missing %d events (buffer overflows: reduce concurrency or increase buffer size)",
			int32(expected)-eventCount.Load(),
		)
	})

	t.Run("EventDataIntegrity", func(t *testing.T) {
		logger := &testLogger{
			Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})),
		}

		bus := event.NewEventBus(logger.Logger, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		type complexData struct {
			ID    string
			Bytes int64
			Error error
		}
		expected := complexData{ID: "123", Bytes: 1024}

		ch := bus.Subscribe(ctx, event.FileMergingCompleted)
		bus.Emit(event.FileMergingCompleted, expected)

		select {
		case e := <-ch:
			received, ok := e.Data.(complexData)
			require.True(t, ok, "Type assertion failed")
			assert.Equal(t, expected, received)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Did not receive event")
		}
	})

	t.Run("NoSubscribersNoPanic", func(t *testing.T) {
		logger := &testLogger{
			Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})),
		}

		bus := event.NewEventBus(logger.Logger, 10)

		assert.NotPanics(t, func() {
			bus.Emit(event.SystemWarning, "test")
		})
	})
}
