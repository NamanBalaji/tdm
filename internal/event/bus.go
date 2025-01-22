package event

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type EventBus interface {
	Subscribe(ctx context.Context, et EventType) <-chan Event
	Emit(et EventType, data interface{})
}

type eventBus struct {
	logger      *slog.Logger
	mu          sync.RWMutex
	subscribers map[EventType][]chan Event
	bufferSize  int
}

func NewEventBus(logger *slog.Logger, bufferSize int) EventBus {
	return &eventBus{
		logger:      logger,
		subscribers: make(map[EventType][]chan Event),
		bufferSize:  bufferSize,
	}
}

func (b *eventBus) Subscribe(ctx context.Context, eventType EventType) <-chan Event {
	b.mu.Lock()
	ch := make(chan Event, b.bufferSize)
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	defer b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.unsubscribe(eventType, ch)
	}()

	return ch
}

func (b *eventBus) Emit(eventType EventType, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	event := Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	for _, ch := range b.subscribers[eventType] {
		select {
		case ch <- event:
		default:
			b.logger.Warn("event channel full, dropping event",
				"event_type", eventType,
				"subscribers", len(b.subscribers[eventType]),
			)
		}
	}
}

func (b *eventBus) unsubscribe(eventType EventType, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[eventType]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			close(ch)

			return
		}
	}
}
