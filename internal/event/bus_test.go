package event

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func setupTestBus() EventBus {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	return NewEventBus(EventBusConfig{
		Logger:     logger,
		BufferSize: 10,
	})
}

func TestGetSubscription(t *testing.T) {
	bus := setupTestBus()
	ctx := context.Background()

	tests := []struct {
		name    string
		types   []EventType
		wantErr bool
	}{
		{
			name:    "valid single type",
			types:   []EventType{DownloadStarted},
			wantErr: false,
		},
		{
			name:    "valid multiple types",
			types:   []EventType{DownloadStarted, DownloadProgress},
			wantErr: false,
		},
		{
			name:    "no types",
			types:   []EventType{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, err := bus.GetSubscription(ctx, tt.types...)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetSubscription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && sub == nil {
				t.Error("GetSubscription() returned nil subscription")
			}
			if !tt.wantErr {
				if len(sub.Types) != len(tt.types) {
					t.Errorf("GetSubscription() wrong number of types = %v, want %v", len(sub.Types), len(tt.types))
				}
			}
		})
	}
}

func TestContextCancellation(t *testing.T) {
	bus := setupTestBus()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test GetSubscription with cancelled context
	sub, err := bus.GetSubscription(ctx, DownloadStarted)
	if err == nil {
		t.Error("GetSubscription() should fail with cancelled context")
	}

	// Test Publish with cancelled context
	err = bus.Publish(ctx, Event{Type: DownloadStarted})
	if err == nil {
		t.Error("Publish() should fail with cancelled context")
	}

	if sub != nil {
		t.Error("Subscription should be nil when context is cancelled")
	}
}

func TestPublishAndReceive(t *testing.T) {
	bus := setupTestBus()
	ctx := context.Background()

	// Create subscription
	sub, err := bus.GetSubscription(ctx, DownloadStarted)
	if err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}

	// Test event
	testPayload := "test"
	evt := Event{
		Type:    DownloadStarted,
		Payload: testPayload,
	}

	// Publish event
	if err := bus.Publish(ctx, evt); err != nil {
		t.Fatalf("Failed to publish event: %v", err)
	}

	// Receive event with timeout
	select {
	case received := <-sub.Ch:
		if received.Type != evt.Type {
			t.Errorf("Wrong event type received = %v, want %v", received.Type, evt.Type)
		}
		if received.Payload != testPayload {
			t.Errorf("Wrong payload received = %v, want %v", received.Payload, testPayload)
		}
		if received.Timestamp.IsZero() {
			t.Error("Timestamp should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := setupTestBus()
	ctx := context.Background()

	// Create multiple subscriptions
	sub1, _ := bus.GetSubscription(ctx, DownloadStarted)
	sub2, _ := bus.GetSubscription(ctx, DownloadStarted)

	evt := Event{Type: DownloadStarted, Payload: "test"}

	// Publish event
	if err := bus.Publish(ctx, evt); err != nil {
		t.Fatalf("Failed to publish event: %v", err)
	}

	// Check both subscribers receive the event
	for i, sub := range []*Subscription{sub1, sub2} {
		select {
		case received := <-sub.Ch:
			if received.Type != evt.Type {
				t.Errorf("Subscriber %d: wrong event type = %v, want %v", i+1, received.Type, evt.Type)
			}
		case <-time.After(time.Second):
			t.Errorf("Subscriber %d: timeout waiting for event", i+1)
		}
	}
}

func TestRemoveSubscription(t *testing.T) {
	bus := setupTestBus()
	ctx := context.Background()

	sub, _ := bus.GetSubscription(ctx, DownloadStarted)

	err := bus.RemoveSubscription(sub)
	if err != nil {
		t.Errorf("RemoveSubscription() error = %v", err)
	}

	if _, ok := <-sub.Ch; ok {
		t.Error("Channel should be closed after removal")
	}

	err = bus.Publish(ctx, Event{Type: DownloadStarted})
	if err != nil {
		t.Errorf("Publish() after removal error = %v", err)
	}

	if err := bus.RemoveSubscription(nil); err != nil {
		t.Errorf("RemoveSubscription(nil) error = %v", err)
	}
}

func TestClose(t *testing.T) {
	bus := setupTestBus()
	ctx := context.Background()

	// Create subscriptions
	sub1, _ := bus.GetSubscription(ctx, DownloadStarted)
	sub2, _ := bus.GetSubscription(ctx, DownloadProgress)

	// Close bus
	if err := bus.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for i, sub := range []*Subscription{sub1, sub2} {
		if _, ok := <-sub.Ch; ok {
			t.Errorf("Subscription %d channel should be closed", i+1)
		}
	}

	if _, err := bus.GetSubscription(ctx, DownloadStarted); err == nil {
		t.Error("GetSubscription() should fail after close")
	}
	if err := bus.Publish(ctx, Event{Type: DownloadStarted}); err == nil {
		t.Error("Publish() should fail after close")
	}
	if err := bus.RemoveSubscription(sub1); err == nil {
		t.Error("RemoveSubscription() should fail after close")
	}
}

func TestConcurrentOperations(t *testing.T) {
	bus := setupTestBus()
	ctx := context.Background()

	const (
		numPublishers  = 10
		numSubscribers = 5
		eventsPerPub   = 100
	)

	var subs []*Subscription
	for i := 0; i < numSubscribers; i++ {
		sub, _ := bus.GetSubscription(ctx, DownloadProgress)
		subs = append(subs, sub)
	}

	done := make(chan struct{})

	received := make([]int, numSubscribers)
	var receiverWg sync.WaitGroup

	for i := 0; i < numSubscribers; i++ {
		receiverWg.Add(1)
		go func(id int, sub *Subscription) {
			defer receiverWg.Done()
			for {
				select {
				case _, ok := <-sub.Ch:
					if !ok {
						return
					}
					received[id]++
				case <-sub.Done:
					return
				case <-done:
					return
				}
			}
		}(i, subs[i])
	}

	var publisherWg sync.WaitGroup
	for i := 0; i < numPublishers; i++ {
		publisherWg.Add(1)
		go func(id int) {
			defer publisherWg.Done()
			for j := 0; j < eventsPerPub; j++ {
				select {
				case <-done:
					return
				default:
					evt := Event{
						Type:    DownloadProgress,
						Payload: j,
					}
					if err := bus.Publish(ctx, evt); err != nil {
						t.Logf("Publisher %d failed: %v", id, err)
					}
					time.Sleep(time.Microsecond)
				}
			}
		}(i)
	}

	publisherWg.Wait()

	close(done)

	for _, sub := range subs {
		_ = bus.RemoveSubscription(sub)
	}

	receiverWg.Wait()

	totalReceived := 0
	for i, count := range received {
		totalReceived += count
		t.Logf("Subscriber %d received %d events", i, count)
	}

	if totalReceived == 0 {
		t.Error("No events were received")
	}
}
