// Package events provides a bounded process-lifetime operational event bus.
package events

import (
	"context"
	"sync"

	publicevent "github.com/nguyenduytan/proxysieve/pkg/event"
)

type Bus struct {
	mu          sync.Mutex
	events      []publicevent.Event
	max         int
	dropped     uint64
	next        uint64
	subscribers map[uint64]chan publicevent.Event
}

func New(max int) (*Bus, error) {
	if max < 1 || max > 1_000_000 {
		return nil, publicevent.ErrInvalid
	}
	return &Bus{max: max}, nil
}

func (b *Bus) Publish(ctx context.Context, event publicevent.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !event.Validate() {
		return publicevent.ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == b.max {
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = event
		b.dropped++
	} else {
		b.events = append(b.events, event)
	}
	for id, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(b.subscribers, id)
		}
	}
	return nil
}

func (b *Bus) Snapshot(limit int) ([]publicevent.Event, uint64, error) {
	if limit < 1 || limit > 1000 {
		return nil, 0, publicevent.ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	start := max(0, len(b.events)-limit)
	items := make([]publicevent.Event, len(b.events)-start)
	for index := range items {
		items[index] = b.events[len(b.events)-1-index]
	}
	return items, b.dropped, nil
}

func (b *Bus) Subscribe(ctx context.Context) <-chan publicevent.Event {
	if ctx.Err() != nil {
		return nil
	}
	b.mu.Lock()
	if len(b.subscribers) >= 128 {
		b.mu.Unlock()
		return nil
	}
	if b.subscribers == nil {
		b.subscribers = map[uint64]chan publicevent.Event{}
	}
	b.next++
	id := b.next
	stream := make(chan publicevent.Event, 64)
	b.subscribers[id] = stream
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if subscriber, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(subscriber)
		}
		b.mu.Unlock()
	}()
	return stream
}
