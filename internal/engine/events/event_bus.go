package events

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type EventBus interface {
	// Subscribe creates a new subscription for specified event types
	Subscribe(types ...EventType) (*Subscription, error)

	// Unsubscribe removes a subscription
	Unsubscribe(*Subscription) error

	// Publish sends an event to all relevant subscribers
	Publish(Event) error

	// Close shuts down the event bus
	Close() error
}

// eventBus implements the EventBus interface
type eventBus struct {
	logger      *slog.Logger
	subscribers sync.Map // map[EventType][]*Subscription
	mu          sync.RWMutex
	closed      bool
}

// NewEventBus creates a new event bus instance
func NewEventBus(logger *slog.Logger) EventBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &eventBus{
		logger: logger,
	}
}

func (b *eventBus) Subscribe(types ...EventType) (*Subscription, error) {
	if len(types) == 0 {
		b.logger.Error("subscription failed: no event types specified")
		return nil, fmt.Errorf("at least one event type must be specified")
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		b.logger.Error("subscription failed: event bus is closed")
		return nil, fmt.Errorf("event bus is closed")
	}
	b.mu.RUnlock()

	sub := &Subscription{
		Types: types,
		Ch:    make(chan Event, 100),
	}

	for _, t := range types {
		subs, _ := b.subscribers.LoadOrStore(t, make([]*Subscription, 0))
		currentSubs := subs.([]*Subscription)

		b.mu.Lock()
		currentSubs = append(currentSubs, sub)
		b.subscribers.Store(t, currentSubs)
		b.mu.Unlock()

		b.logger.Debug("subscribed to event type",
			"event_type", t,
			"total_subscribers", len(currentSubs)+1)
	}

	b.logger.Info("new subscription created",
		"event_types", types,
		"subscriber_count", len(types))
	return sub, nil
}

func (b *eventBus) Unsubscribe(sub *Subscription) error {
	if sub == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if sub.closed {
		return nil
	}

	for _, t := range sub.Types {
		if subs, ok := b.subscribers.Load(t); ok {
			currentSubs := subs.([]*Subscription)
			newSubs := make([]*Subscription, 0, len(currentSubs)-1)

			for _, s := range currentSubs {
				if s != sub {
					newSubs = append(newSubs, s)
				}
			}

			if len(newSubs) == 0 {
				b.subscribers.Delete(t)
				b.logger.Debug("removed last subscriber for event type",
					"event_type", t)
			} else {
				b.subscribers.Store(t, newSubs)
				b.logger.Debug("removed subscriber from event type",
					"event_type", t,
					"remaining_subscribers", len(newSubs))
			}
		}
	}

	close(sub.Ch)
	sub.closed = true

	b.logger.Info("subscription unsubscribed",
		"event_types", sub.Types)
	return nil
}

func (b *eventBus) Publish(evt Event) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		b.logger.Error("publish failed: event bus is closed",
			"event_type", evt.Type,
			"event_id", evt.ID)
		return fmt.Errorf("event bus is closed")
	}
	b.mu.RUnlock()

	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	if subs, ok := b.subscribers.Load(evt.Type); ok {
		subscribers := subs.([]*Subscription)
		successCount := 0
		skipCount := 0

		for _, sub := range subscribers {
			if sub.closed {
				continue
			}

			// Non-blocking send to prevent slow subscribers from blocking others
			select {
			case sub.Ch <- evt:
				successCount++
			default:
				skipCount++
				b.logger.Warn("skipped slow subscriber",
					"event_type", evt.Type,
					"event_id", evt.ID)
			}
		}

		if successCount > 0 {
			b.logger.Debug("event published",
				"event_type", evt.Type,
				"event_id", evt.ID,
				"total_subscribers", len(subscribers),
				"successful_deliveries", successCount,
				"skipped_deliveries", skipCount)
		} else if skipCount > 0 {
			b.logger.Warn("event delivery failed for all subscribers",
				"event_type", evt.Type,
				"event_id", evt.ID,
				"total_subscribers", len(subscribers),
				"skipped_deliveries", skipCount)
		}
	} else {
		b.logger.Debug("no subscribers for event",
			"event_type", evt.Type,
			"event_id", evt.ID)
	}

	return nil
}

func (b *eventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true
	closedCount := 0
	totalSubscribers := 0

	// Close all subscriptions
	b.subscribers.Range(func(key, value interface{}) bool {
		eventType := key.(EventType)
		subs := value.([]*Subscription)
		totalSubscribers += len(subs)

		for _, sub := range subs {
			if !sub.closed {
				close(sub.Ch)
				sub.closed = true
				closedCount++
			}
		}

		b.logger.Debug("closing subscriptions for event type",
			"event_type", eventType,
			"subscriber_count", len(subs))
		return true
	})

	b.logger.Info("event bus closed",
		"total_subscribers", totalSubscribers,
		"subscriptions_closed", closedCount)
	return nil
}
