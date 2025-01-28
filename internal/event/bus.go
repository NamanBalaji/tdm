package event

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EventBus is the interface for the event bus.
type EventBus interface {
	Subscribe(ctx context.Context, et EventType) <-chan Event
	Emit(et EventType, data any)
}

// eventBus is the implementation of the EventBus interface.
type eventBus struct {
	logger      *slog.Logger
	subscribers *sync.Map // EventType → []*subscriber
	bufferSize  int
}

// NewEventBus creates a new EventBus instance.
func NewEventBus(logger *slog.Logger, bufferSize int) EventBus {
	return &eventBus{
		logger:      logger,
		subscribers: &sync.Map{},
		bufferSize:  bufferSize,
	}
}

// Subscribe subscribes to events of a specific type.
func (b *eventBus) Subscribe(ctx context.Context, et EventType) <-chan Event {
	ch := make(chan Event, b.bufferSize)
	sub := &subscriber{ch: ch}

	// Load or create subscriber list for this event type
	rawSubs, _ := b.subscribers.LoadOrStore(et, []*subscriber{})
	subs := rawSubs.([]*subscriber)
	subs = append(subs, sub)
	b.subscribers.Store(et, subs)

	// Cleanup on context cancellation
	go func() {
		<-ctx.Done()
		sub.closed.Store(true)
		close(ch)
		b.cleanupSubscriber(et, sub)
	}()

	return ch
}

// Emit emits an event of a specific type with associated data.
func (b *eventBus) Emit(et EventType, data any) {
	rawSubs, ok := b.subscribers.Load(et)
	if !ok {
		return // No subscribers
	}

	subs := rawSubs.([]*subscriber)
	event := Event{
		Type:      et,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	for _, sub := range subs {
		if sub.closed.Load() {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			b.logger.Warn("event channel full", "event_type", et)
		}
	}
}

// cleanupSubscriber removes a subscriber from the list for a specific event type.
func (b *eventBus) cleanupSubscriber(et EventType, target *subscriber) {
	rawSubs, _ := b.subscribers.Load(et)
	subs := rawSubs.([]*subscriber)

	newSubs := make([]*subscriber, 0, len(subs))
	for _, sub := range subs {
		if sub != target {
			newSubs = append(newSubs, sub)
		}
	}

	b.subscribers.Store(et, newSubs)
}
