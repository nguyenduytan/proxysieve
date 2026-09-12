// Package budget implements synchronized hard-budget byte reservations.
package budget

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"slices"
	"sync"
)

var ErrNotFound = errors.New("budget not found")

type Manager struct {
	mu             sync.Mutex
	configs        map[model.ID]budget.Config
	usage          map[model.ID]budget.Usage
	maxReservation traffic.Bytes
	store          Store
}

type Store interface {
	ReserveBudgets(context.Context, []budget.Config, traffic.Bytes) error
	ConsumeBudgets(context.Context, []model.ID, traffic.Bytes) error
	ReleaseBudgets(context.Context, []model.ID, traffic.Bytes) error
	BudgetUsage(context.Context, model.ID) (budget.Usage, error)
	RecoverBudgetReservations(context.Context) error
}

func New(configs []budget.Config, maxReservation traffic.Bytes) (*Manager, error) {
	return NewPersistent(configs, maxReservation, nil)
}

func NewPersistent(configs []budget.Config, maxReservation traffic.Bytes, store Store) (*Manager, error) {
	if maxReservation == 0 || maxReservation > 1<<40 {
		return nil, budget.ErrInvalid
	}
	m := &Manager{configs: map[model.ID]budget.Config{}, usage: map[model.ID]budget.Usage{}, maxReservation: maxReservation, store: store}
	for _, config := range configs {
		if config.Validate() != nil {
			return nil, budget.ErrInvalid
		}
		if _, ok := m.configs[config.ID]; ok {
			return nil, budget.ErrInvalid
		}
		m.configs[config.ID] = config
	}
	if store != nil {
		if err := store.RecoverBudgetReservations(context.Background()); err != nil {
			return nil, err
		}
	}
	return m, nil
}

type Lease struct {
	mu        sync.Mutex
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
	configs := make([]budget.Config, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, budget.ErrInvalid
		}
		seen[id] = true
		config, ok := m.configs[id]
		if !ok {
			return nil, ErrNotFound
		}
		configs = append(configs, config)
		if m.store == nil {
			usage := m.usage[id]
			if config.Hard && wouldExceed(usage, amount, config.Limit) {
				return nil, budget.ErrExceeded
			}
			if usage.Reserved > ^traffic.Bytes(0)-amount {
				return nil, budget.ErrExceeded
			}
		}
	}
	if m.store != nil {
		if err := m.store.ReserveBudgets(ctx, configs, amount); err != nil {
			return nil, err
		}
		return &Lease{manager: m, ids: append([]model.ID(nil), ids...), remaining: amount}, nil
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
func (l *Lease) Consume(ctx context.Context, amount traffic.Bytes) (traffic.Bytes, error) {
	if l == nil || l.manager == nil {
		return 0, budget.ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if l.closed {
		return 0, budget.ErrClosed
	}
	allowed := min(amount, l.remaining)
	if l.manager.store != nil {
		if err := l.manager.store.ConsumeBudgets(ctx, l.ids, allowed); err != nil {
			return 0, err
		}
		l.remaining -= allowed
		if allowed < amount {
			return allowed, budget.ErrExceeded
		}
		return allowed, nil
	}
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
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
func (l *Lease) Close(ctx context.Context) error {
	if l == nil || l.manager == nil {
		return budget.ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return budget.ErrClosed
	}
	if l.manager.store != nil {
		if err := l.manager.store.ReleaseBudgets(ctx, l.ids, l.remaining); err != nil {
			return err
		}
		l.remaining = 0
		l.closed = true
		return nil
	}
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
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
	return m.UsageContext(context.Background(), id)
}
func (m *Manager) UsageContext(ctx context.Context, id model.ID) (budget.Usage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.configs[id]; !ok {
		return budget.Usage{}, ErrNotFound
	}
	if m.store != nil {
		return m.store.BudgetUsage(ctx, id)
	}
	return m.usage[id], nil
}

func (m *Manager) ApplicableIDs(clientID, poolID, proxyID model.ID) []model.ID {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]model.ID, 0, len(m.configs))
	for id, configured := range m.configs {
		if configured.Applies(clientID, poolID, proxyID) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
func wouldExceed(usage budget.Usage, amount, limit traffic.Bytes) bool {
	if usage.Used > limit || usage.Reserved > limit-usage.Used {
		return true
	}
	return amount > limit-usage.Used-usage.Reserved
}
