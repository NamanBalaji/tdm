// internal/engine/events/bus_test.go
package events

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestEventBus() EventBus {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	return NewEventBus(logger)
}

func TestEventBus_Subscribe(t *testing.T) {
	tests := []struct {
		name        string
		types       []EventType
		expectError bool
	}{
		{
			name:        "valid subscription single type",
			types:       []EventType{DownloadStarted},
			expectError: false,
		},
		{
			name:        "valid subscription multiple types",
			types:       []EventType{DownloadStarted, DownloadProgress, DownloadCompleted},
			expectError: false,
		},
		{
			name:        "invalid subscription no types",
			types:       []EventType{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := setupTestEventBus()
			defer bus.Close()

			sub, err := bus.Subscribe(tt.types...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, sub)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sub)
				assert.Equal(t, tt.types, sub.Types)
				assert.NotNil(t, sub.Ch)
			}
		})
	}
}

func TestEventBus_Publish(t *testing.T) {
	bus := setupTestEventBus()
	defer bus.Close()

	// Subscribe to multiple event types
	sub1, err := bus.Subscribe(DownloadStarted, DownloadProgress)
	require.NoError(t, err)

	sub2, err := bus.Subscribe(DownloadProgress)
	require.NoError(t, err)

	id := uuid.New()
	events := []Event{
		{
			Type:      DownloadStarted,
			ID:        id,
			Timestamp: time.Now(),
			Data:      "start data",
		},
		{
			Type:      DownloadProgress,
			ID:        id,
			Timestamp: time.Now(),
			Data:      "progress data",
		},
	}

	for _, evt := range events {
		err := bus.Publish(evt)
		assert.NoError(t, err)
	}

	for i := 0; i < 2; i++ {
		select {
		case evt := <-sub1.Ch:
			assert.Contains(t, []EventType{DownloadStarted, DownloadProgress}, evt.Type)
			assert.Equal(t, id, evt.ID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event on sub1")
		}
	}

	select {
	case evt := <-sub2.Ch:
		assert.Equal(t, DownloadProgress, evt.Type)
		assert.Equal(t, id, evt.ID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event on sub2")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := setupTestEventBus()
	defer bus.Close()

	sub, err := bus.Subscribe(DownloadStarted, DownloadProgress)
	require.NoError(t, err)

	err = bus.Unsubscribe(sub)
	assert.NoError(t, err)

	_, ok := <-sub.Ch
	assert.False(t, ok, "channel should be closed")

	assert.True(t, sub.closed)

	err = bus.Unsubscribe(sub)
	assert.NoError(t, err)
}

func TestEventBus_Close(t *testing.T) {
	bus := setupTestEventBus()

	sub1, _ := bus.Subscribe(DownloadStarted)
	sub2, _ := bus.Subscribe(DownloadProgress)

	err := bus.Close()
	assert.NoError(t, err)

	_, ok1 := <-sub1.Ch
	assert.False(t, ok1, "sub1 channel should be closed")
	_, ok2 := <-sub2.Ch
	assert.False(t, ok2, "sub2 channel should be closed")

	sub3, err := bus.Subscribe(DownloadStarted)
	assert.Error(t, err)
	assert.Nil(t, sub3)

	err = bus.Publish(Event{Type: DownloadStarted})
	assert.Error(t, err)
}

func TestEventBus_FullChannel(t *testing.T) {
	bus := setupTestEventBus()
	defer bus.Close()

	_, err := bus.Subscribe(DownloadProgress)
	require.NoError(t, err)

	for i := 0; i < 101; i++ { // More than buffer size
		err := bus.Publish(Event{
			Type: DownloadProgress,
			ID:   uuid.New(),
			Data: i,
		})
		assert.NoError(t, err)
	}

	err = bus.Publish(Event{Type: DownloadProgress})
	assert.NoError(t, err)
}

func TestEventBus_ConcurrentOperations(t *testing.T) {
	bus := setupTestEventBus()
	defer bus.Close()

	sub, err := bus.Subscribe(DownloadProgress)
	require.NoError(t, err)

	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				err := bus.Publish(Event{
					Type: DownloadProgress,
					ID:   uuid.New(),
				})
				assert.NoError(t, err)
			}
			done <- true
		}()
	}

	for i := 0; i < 3; i++ {
		go func() {
			_, err := bus.Subscribe(DownloadProgress)
			assert.NoError(t, err)
			done <- true
		}()
	}

	for i := 0; i < 8; i++ {
		<-done
	}

	err = bus.Unsubscribe(sub)
	assert.NoError(t, err)
}

func TestEventBus_NoSubscribers(t *testing.T) {
	bus := setupTestEventBus()
	defer bus.Close()

	err := bus.Publish(Event{
		Type: DownloadStarted,
		ID:   uuid.New(),
	})
	assert.NoError(t, err)
}

func TestEventBus_NilSubscriptionUnsubscribe(t *testing.T) {
	bus := setupTestEventBus()
	defer bus.Close()

	err := bus.Unsubscribe(nil)
	assert.NoError(t, err)
}
