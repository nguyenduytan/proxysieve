package audit

import (
	"context"
	"sort"
	"sync"
)

// Memory is bounded and suited to focused tests or ephemeral development mode.
type Memory struct {
	mu     sync.Mutex
	events []Event
	max    int
}

func NewMemory(max int) (*Memory, error) {
	if max < 1 || max > 100_000 {
		return nil, ErrInvalid
	}
	return &Memory{max: max}, nil
}
func (m *Memory) Record(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !event.Validate() {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) >= m.max {
		m.events = append(m.events[1:], event)
	} else {
		m.events = append(m.events, event)
	}
	return nil
}
func (m *Memory) ListAudit(ctx context.Context, page Page) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !page.Valid() {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	events := append([]Event(nil), m.events...)
	sort.Slice(events, func(i, j int) bool {
		if events[i].At.Equal(events[j].At) {
			return events[i].ID > events[j].ID
		}
		return events[i].At.After(events[j].At)
	})
	out := make([]Event, 0, page.Limit)
	for _, event := range events {
		if !page.Before.IsZero() {
			if event.At.After(page.Before) {
				continue
			}
			if event.At.Equal(page.Before) {
				if page.BeforeID == "" || event.ID >= page.BeforeID {
					continue
				}
			}
		}
		out = append(out, event)
		if len(out) == page.Limit {
			break
		}
	}
	return out, nil
}

var _ Writer = (*Memory)(nil)
var _ Reader = (*Memory)(nil)
