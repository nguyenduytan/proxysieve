// Package budget implements synchronized hard-budget byte reservations.
package budget

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicstore "github.com/nguyenduytan/proxysieve/pkg/store"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrNotFound = errors.New("budget not found")

type Manager struct {
	mu             sync.Mutex
	configs        map[model.ID]budget.Config
	revisions      map[model.ID]int64
	usage          map[usageKey]budget.Usage
	maxReservation traffic.Bytes
	store          Store
	now            func() time.Time
}

type Store interface {
	publicstore.Budgets
	ReserveBudgets(context.Context, []budget.Config, time.Time, traffic.Bytes) error
	ConsumeBudgets(context.Context, []budget.Config, time.Time, traffic.Bytes) error
	ReleaseBudgets(context.Context, []budget.Config, time.Time, traffic.Bytes) error
	BudgetUsage(context.Context, budget.Config, time.Time) (budget.Usage, error)
	RecoverBudgetReservations(context.Context) error
}

type usageKey struct {
	id          model.ID
	windowStart int64
}

type Status struct {
	budget.Config
	Revision    int64         `json:"revision"`
	Used        traffic.Bytes `json:"used_bytes"`
	Reserved    traffic.Bytes `json:"reserved_bytes"`
	Remaining   traffic.Bytes `json:"remaining_bytes"`
	Exhausted   bool          `json:"exhausted"`
	WindowStart *time.Time    `json:"window_start,omitempty"`
	WindowEnd   *time.Time    `json:"window_end,omitempty"`
}

func New(configs []budget.Config, maxReservation traffic.Bytes) (*Manager, error) {
	return NewPersistent(configs, maxReservation, nil)
}

func NewPersistent(configs []budget.Config, maxReservation traffic.Bytes, store Store) (*Manager, error) {
	if maxReservation == 0 || maxReservation > 1<<40 {
		return nil, budget.ErrInvalid
	}
	m := &Manager{configs: map[model.ID]budget.Config{}, revisions: map[model.ID]int64{}, usage: map[usageKey]budget.Usage{}, maxReservation: maxReservation, store: store, now: time.Now}
	for _, config := range configs {
		if config.Validate() != nil {
			return nil, budget.ErrInvalid
		}
		if _, ok := m.configs[config.ID]; ok {
			return nil, budget.ErrInvalid
		}
		m.configs[config.ID] = config
		m.revisions[config.ID] = 1
	}
	if store != nil {
		records, err := store.InitializeBudgets(context.Background(), configs)
		if err != nil {
			return nil, err
		}
		m.configs = make(map[model.ID]budget.Config, len(records))
		m.revisions = make(map[model.ID]int64, len(records))
		for _, record := range records {
			m.configs[record.Budget.ID] = record.Budget
			m.revisions[record.Budget.ID] = record.Revision
		}
		if err := store.RecoverBudgetReservations(context.Background()); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) PutConfig(ctx context.Context, configured budget.Config, expected int64) (publicstore.BudgetRecord, error) {
	if m == nil || configured.Validate() != nil || expected < 0 {
		return publicstore.BudgetRecord{}, publicstore.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return publicstore.BudgetRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.configs[configured.ID]
	if expected == 0 && exists {
		return publicstore.BudgetRecord{}, publicstore.ErrConflict
	}
	if expected > 0 {
		if !exists {
			return publicstore.BudgetRecord{}, publicstore.ErrNotFound
		}
		if m.revisions[configured.ID] != expected {
			return publicstore.BudgetRecord{}, publicstore.ErrConflict
		}
	}
	record := publicstore.BudgetRecord{Budget: configured, Revision: expected + 1}
	if m.store != nil {
		var err error
		record, err = m.store.PutBudget(ctx, configured, expected)
		if err != nil {
			return publicstore.BudgetRecord{}, err
		}
	}
	m.configs[configured.ID] = configured
	m.revisions[configured.ID] = record.Revision
	return record, nil
}

func (m *Manager) DeleteConfig(ctx context.Context, id model.ID, expected int64) error {
	if m == nil || !id.Valid() || expected < 1 {
		return publicstore.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.configs[id]; !exists {
		return publicstore.ErrNotFound
	}
	if m.revisions[id] != expected {
		return publicstore.ErrConflict
	}
	if m.store != nil {
		if err := m.store.DeleteBudget(ctx, id, expected); err != nil {
			return err
		}
	}
	delete(m.configs, id)
	delete(m.revisions, id)
	return nil
}

type Lease struct {
	mu        sync.Mutex
	manager   *Manager
	configs   []budget.Config
	at        time.Time
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
	at := m.now().UTC()
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
			key, keyErr := budgetUsageKey(config, at)
			if keyErr != nil {
				return nil, keyErr
			}
			usage := m.usage[key]
			if config.Hard && wouldExceed(usage, amount, config.Limit) {
				return nil, budget.ErrExceeded
			}
			if usage.Reserved > ^traffic.Bytes(0)-amount {
				return nil, budget.ErrExceeded
			}
		}
	}
	if m.store != nil {
		if err := m.store.ReserveBudgets(ctx, configs, at, amount); err != nil {
			return nil, err
		}
		return &Lease{manager: m, configs: configs, at: at, remaining: amount}, nil
	}
	for _, config := range configs {
		key, _ := budgetUsageKey(config, at)
		usage := m.usage[key]
		usage.Reserved += amount
		m.usage[key] = usage
	}
	return &Lease{manager: m, configs: configs, at: at, remaining: amount}, nil
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
		if err := l.manager.store.ConsumeBudgets(ctx, l.configs, l.at, allowed); err != nil {
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
	for _, config := range l.configs {
		key, _ := budgetUsageKey(config, l.at)
		usage := l.manager.usage[key]
		usage.Reserved -= allowed
		usage.Used += allowed
		l.manager.usage[key] = usage
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
		if err := l.manager.store.ReleaseBudgets(ctx, l.configs, l.at, l.remaining); err != nil {
			return err
		}
		l.remaining = 0
		l.closed = true
		return nil
	}
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	for _, config := range l.configs {
		key, _ := budgetUsageKey(config, l.at)
		usage := l.manager.usage[key]
		usage.Reserved -= l.remaining
		l.manager.usage[key] = usage
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
	config, ok := m.configs[id]
	if !ok {
		return budget.Usage{}, ErrNotFound
	}
	at := m.now().UTC()
	return m.usageLocked(ctx, config, at)
}

func (m *Manager) Statuses(ctx context.Context) ([]Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	at := m.now().UTC()
	ids := make([]model.ID, 0, len(m.configs))
	for id := range m.configs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	statuses := make([]Status, 0, len(ids))
	for _, id := range ids {
		status, err := m.statusLocked(ctx, id, at)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (m *Manager) Status(ctx context.Context, id model.ID) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(ctx, id, m.now().UTC())
}

func (m *Manager) statusLocked(ctx context.Context, id model.ID, at time.Time) (Status, error) {
	config, ok := m.configs[id]
	if !ok {
		return Status{}, publicstore.ErrNotFound
	}
	usage, err := m.usageLocked(ctx, config, at)
	if err != nil {
		return Status{}, err
	}
	start, end, err := config.WindowBounds(at)
	if err != nil {
		return Status{}, err
	}
	if config.Scope == "" {
		config.Scope = budget.ScopeSystem
	}
	if config.Window == "" {
		config.Window = budget.WindowLifetime
	}
	remaining := traffic.Bytes(0)
	if usage.Used < config.Limit && usage.Reserved < config.Limit-usage.Used {
		remaining = config.Limit - usage.Used - usage.Reserved
	}
	status := Status{Config: config, Revision: m.revisions[id], Used: usage.Used, Reserved: usage.Reserved, Remaining: remaining, Exhausted: remaining == 0}
	if !start.IsZero() {
		status.WindowStart, status.WindowEnd = &start, &end
	}
	return status, nil
}

func (m *Manager) usageLocked(ctx context.Context, config budget.Config, at time.Time) (budget.Usage, error) {
	if m.store != nil {
		return m.store.BudgetUsage(ctx, config, at)
	}
	key, err := budgetUsageKey(config, at)
	if err != nil {
		return budget.Usage{}, err
	}
	return m.usage[key], nil
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

func (m *Manager) References(scope budget.Scope, id model.ID) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, configured := range m.configs {
		if configured.Scope == scope && configured.ScopeID == id {
			return true
		}
	}
	return false
}
func wouldExceed(usage budget.Usage, amount, limit traffic.Bytes) bool {
	if usage.Used > limit || usage.Reserved > limit-usage.Used {
		return true
	}
	return amount > limit-usage.Used-usage.Reserved
}

func budgetUsageKey(config budget.Config, at time.Time) (usageKey, error) {
	start, _, err := config.WindowBounds(at)
	if err != nil {
		return usageKey{}, err
	}
	key := usageKey{id: config.ID}
	if !start.IsZero() {
		key.windowStart = start.UnixNano()
	}
	return key, nil
}
