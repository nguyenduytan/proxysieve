// Package budget implements synchronized hard-budget byte reservations.
package budget

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"sync"
)

var ErrNotFound = errors.New("budget not found")

type Manager struct {
	mu             sync.Mutex
	configs        map[model.ID]budget.Config
	usage          map[model.ID]budget.Usage
	maxReservation traffic.Bytes
}

func New(configs []budget.Config, maxReservation traffic.Bytes) (*Manager, error) {
	if maxReservation == 0 || maxReservation > 1<<40 {
		return nil, budget.ErrInvalid
	}
	m := &Manager{configs: map[model.ID]budget.Config{}, usage: map[model.ID]budget.Usage{}, maxReservation: maxReservation}
	for _, config := range configs {
		if config.Validate() != nil {
			return nil, budget.ErrInvalid
		}
		if _, ok := m.configs[config.ID]; ok {
			return nil, budget.ErrInvalid
		}
		m.configs[config.ID] = config
	}
	return m, nil
}

type Lease struct {
	manager   *Manager
	ids       []model.ID
	remaining traffic.Bytes
	closed    bool
}

// Reserve atomically reserves a maximum transferable amount across every scope.
// If a caller asks for a bigger stream, it must create a bounded reservation again.
func (m *Manager) Reserve(ctx context.Context, ids []model.ID, amount traffic.Bytes) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || amount == 0 || amount > m.maxReservation {
		return nil, budget.ErrInvalid
	}
	seen := map[model.ID]bool{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		if seen[id] {
			return nil, budget.ErrInvalid
		}
		seen[id] = true
		config, ok := m.configs[id]
		if !ok {
			return nil, ErrNotFound
		}
		usage := m.usage[id]
		if config.Hard && wouldExceed(usage, amount, config.Limit) {
			return nil, budget.ErrExceeded
		}
		if usage.Reserved > ^traffic.Bytes(0)-amount {
			return nil, budget.ErrExceeded
		}
	}
	for _, id := range ids {
		usage := m.usage[id]
		usage.Reserved += amount
		m.usage[id] = usage
	}
	return &Lease{manager: m, ids: append([]model.ID(nil), ids...), remaining: amount}, nil
}

// Consume converts reserved bytes into used bytes and never grants more than the
// reservation. Gate stream reads/writes using the returned allowance.
func (l *Lease) Consume(amount traffic.Bytes) (traffic.Bytes, error) {
	if l == nil || l.manager == nil {
		return 0, budget.ErrInvalid
	}
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	if l.closed {
		return 0, budget.ErrClosed
	}
	allowed := amount
	if allowed > l.remaining {
		allowed = l.remaining
	}
	for _, id := range l.ids {
		usage := l.manager.usage[id]
		usage.Reserved -= allowed
		usage.Used += allowed
		l.manager.usage[id] = usage
	}
	l.remaining -= allowed
	if allowed < amount {
		return allowed, budget.ErrExceeded
	}
	return allowed, nil
}
func (l *Lease) Close() error {
	if l == nil || l.manager == nil {
		return budget.ErrInvalid
	}
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	if l.closed {
		return budget.ErrClosed
	}
	for _, id := range l.ids {
		usage := l.manager.usage[id]
		usage.Reserved -= l.remaining
		l.manager.usage[id] = usage
	}
	l.remaining = 0
	l.closed = true
	return nil
}
func (m *Manager) Usage(id model.ID) (budget.Usage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.configs[id]; !ok {
		return budget.Usage{}, ErrNotFound
	}
	return m.usage[id], nil
}
func wouldExceed(usage budget.Usage, amount, limit traffic.Bytes) bool {
	if usage.Used > limit || usage.Reserved > limit-usage.Used {
		return true
	}
	return amount > limit-usage.Used-usage.Reserved
}
