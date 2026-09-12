// Package traffic provides a bounded in-memory event recorder for runtime metrics.
package traffic

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"sync"
)

var ErrFull = errors.New("traffic event buffer full")

type Memory struct {
	mu      sync.Mutex
	events  []traffic.Event
	max     int
	dropped uint64
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
	if len(m.events) >= m.max {
		m.dropped++
		copy(m.events, m.events[1:])
		m.events[len(m.events)-1] = event
		return ErrFull
	}
	m.events = append(m.events, event)
	return nil
}
func (m *Memory) Snapshot() ([]traffic.Event, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]traffic.Event(nil), m.events...), m.dropped
}

var _ traffic.Recorder = (*Memory)(nil)
