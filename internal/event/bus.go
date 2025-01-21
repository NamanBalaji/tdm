package event

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type EventBus interface {
	// GetSubscription creates a new subscription for specified event types
	GetSubscription(ctx context.Context, types ...EventType) (*Subscription, error)
	// RemoveSubscription removes a subscription
	RemoveSubscription(*Subscription) error
	// Publish sends an event to all relevant subscribers
	Publish(ctx context.Context, evt Event) error
	// Close shuts down the event bus
	Close() error
}

type eventBus struct {
	logger     *slog.Logger
	mu         sync.RWMutex
	subs       map[EventType][]*Subscription
	bufferSize int
	closed     bool
	wg         sync.WaitGroup
}

type EventBusConfig struct {
	Logger     *slog.Logger
	BufferSize int
}

func NewEventBus(cfg EventBusConfig) EventBus {
	return &eventBus{
		logger:     cfg.Logger,
		subs:       make(map[EventType][]*Subscription),
		bufferSize: cfg.BufferSize,
	}
}

func (b *eventBus) GetSubscription(ctx context.Context, types ...EventType) (*Subscription, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		if len(types) == 0 {
			b.logger.Error("subscription failed: no event types specified")
			return nil, fmt.Errorf("at least one event type must be specified")
		}

		b.mu.Lock()
		defer b.mu.Unlock()

		if b.closed {
			return nil, fmt.Errorf("event bus is closed")
		}

		sub := &Subscription{
			Types: types,
			Ch:    make(chan Event, b.bufferSize),
			Done:  make(chan struct{}),
		}

		for _, t := range types {
			b.subs[t] = append(b.subs[t], sub)
		}

		b.logger.Info("new subscription created",
			"event_types", types,
			"subscriber_count", len(types))

		return sub, nil
	}
}

func (b *eventBus) RemoveSubscription(sub *Subscription) error {
	if sub == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("event bus is closed")
	}

	// Signal that the subscription is being removed
	close(sub.Done)

	for _, t := range sub.Types {
		currentSubs := b.subs[t]
		newSubs := make([]*Subscription, 0, len(currentSubs))
		for _, s := range currentSubs {
			if s != sub {
				newSubs = append(newSubs, s)
			}
		}
		if len(newSubs) == 0 {
			delete(b.subs, t)
			b.logger.Debug("removed last subscriber for event type", "event_type", t)
		} else {
			b.subs[t] = newSubs
		}
	}

	// Drain any remaining events before closing
	go func() {
		for range sub.Ch {
			// Drain channel
		}
	}()

	close(sub.Ch)
	b.logger.Info("subscription removed", "event_types", sub.Types)
	return nil
}

func (b *eventBus) Publish(ctx context.Context, evt Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		b.mu.RLock()
		defer b.mu.RUnlock()

		if b.closed {
			return fmt.Errorf("event bus is closed")
		}

		if evt.Timestamp.IsZero() {
			evt.Timestamp = time.Now()
		}

		if subs, ok := b.subs[evt.Type]; ok {
			b.wg.Add(1)
			go func(subscribers []*Subscription) {
				defer b.wg.Done()
				for _, sub := range subscribers {
					select {
					case <-ctx.Done():
						return
					case <-sub.Done:
						continue
					default:
						select {
						case sub.Ch <- evt:
						case <-sub.Done:
							continue
						default:
							b.logger.Warn("dropped event: subscriber channel full",
								"event_type", evt.Type)
						}
					}
				}
			}(subs) // Pass subscribers slice to avoid race conditions
		} else {
			b.logger.Debug("no subscribers for event", "event_type", evt.Type)
		}

		return nil
	}
}

func (b *eventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true

	b.wg.Wait()

	for t, subs := range b.subs {
		for _, sub := range subs {
			close(sub.Done)
			close(sub.Ch)
		}
		delete(b.subs, t)
	}

	b.logger.Info("event bus closed")
	return nil
}
