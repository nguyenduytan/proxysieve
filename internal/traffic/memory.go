// Package traffic provides a bounded in-memory event recorder for runtime metrics.
package traffic

import (
	"context"
	"errors"
	"sync"

	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrFull = errors.New("traffic event buffer full")

type Memory struct {
	mu          sync.Mutex
	events      []traffic.Event
	max         int
	dropped     uint64
	next        uint64
	subscribers map[uint64]chan traffic.Event
}

func NewMemory(max int) (*Memory, error) {
	if max < 1 || max > 1_000_000 {
		return nil, ErrFull
	}
	return &Memory{max: max}, nil
}
func (m *Memory) Record(ctx context.Context, event traffic.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	full := len(m.events) >= m.max
	if full {
		m.dropped++
		copy(m.events, m.events[1:])
		m.events[len(m.events)-1] = event
	} else {
		m.events = append(m.events, event)
	}
	for id, subscriber := range m.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(m.subscribers, id)
		}
	}
	if full {
		return ErrFull
	}
	return nil
}
func (m *Memory) Snapshot() ([]traffic.Event, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]traffic.Event(nil), m.events...), m.dropped
}

func (m *Memory) Subscribe(ctx context.Context) <-chan traffic.Event {
	if ctx.Err() != nil {
		return nil
	}
	m.mu.Lock()
	if len(m.subscribers) >= 128 {
		m.mu.Unlock()
		return nil
	}
	if m.subscribers == nil {
		m.subscribers = map[uint64]chan traffic.Event{}
	}
	m.next++
	id := m.next
	stream := make(chan traffic.Event, 64)
	m.subscribers[id] = stream
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		if subscriber, ok := m.subscribers[id]; ok {
			delete(m.subscribers, id)
			close(subscriber)
		}
		m.mu.Unlock()
	}()
	return stream
}

var _ traffic.Recorder = (*Memory)(nil)
