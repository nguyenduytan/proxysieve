package budget

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"sync"
	"testing"
)

func TestReservationsAreAtomicAndReconciled(t *testing.T) {
	m, err := New([]budget.Config{{ID: "system", Name: "System", Limit: 100, Hard: true, Action: budget.ActionReject}, {ID: "client", Name: "Client", Limit: 100, Hard: true, Action: budget.ActionReject}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Reserve(context.Background(), []model.ID{"system", "client"}, 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Reserve(context.Background(), []model.ID{"system", "client"}, 50); !errors.Is(err, budget.ErrExceeded) {
		t.Fatal(err)
	}
	allowed, err := first.Consume(t.Context(), 40)
	if err != nil || allowed != 40 {
		t.Fatal(allowed, err)
	}
	if err = first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"system", "client"} {
		usage, _ := m.Usage(model.ID(id))
		if usage.Used != 40 || usage.Reserved != 0 {
			t.Fatal(id, usage)
		}
	}
}

func TestPersistentReservationsRecoverConservatively(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "budgets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	configs := []budget.Config{{ID: "system", Name: "System", Scope: budget.ScopeSystem, Limit: 100, Hard: true, Action: budget.ActionReject}}
	manager, err := NewPersistent(configs, 100, store)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Reserve(t.Context(), []model.ID{"system"}, 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lease.Consume(t.Context(), 40); err != nil {
		t.Fatal(err)
	}
	// A new process charges the outstanding 20-byte reservation rather than
	// clearing it and making a restart bypass the hard limit.
	manager, err = NewPersistent(configs, 100, store)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := manager.UsageContext(t.Context(), "system")
	if err != nil || usage.Used != 60 || usage.Reserved != 0 {
		t.Fatal(usage, err)
	}
	if _, err = manager.Reserve(t.Context(), []model.ID{"system"}, 41); !errors.Is(err, budget.ErrExceeded) {
		t.Fatal(err)
	}
}

func TestPersistentConcurrentReservationsAndScopes(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "concurrent-budgets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	configs := []budget.Config{
		{ID: "system", Name: "System", Scope: budget.ScopeSystem, Limit: 1000, Hard: true, Action: budget.ActionReject},
		{ID: "client-a", Name: "Client A", Scope: budget.ScopeClient, ScopeID: "client", Limit: 1000, Hard: true, Action: budget.ActionReject},
	}
	manager, err := NewPersistent(configs, 10, store)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.ApplicableIDs("client", "pool", "proxy"); !slices.Equal(got, []model.ID{"client-a", "system"}) {
		t.Fatal(got)
	}
	var granted atomic.Int64
	var group sync.WaitGroup
	for range 200 {
		group.Go(func() {
			lease, reserveErr := manager.Reserve(t.Context(), []model.ID{"system", "client-a"}, 10)
			if reserveErr != nil {
				return
			}
			if allowed, consumeErr := lease.Consume(t.Context(), 10); consumeErr == nil && allowed == 10 {
				granted.Add(10)
			}
			_ = lease.Close(t.Context())
		})
	}
	group.Wait()
	if granted.Load() != 1000 {
		t.Fatal(granted.Load())
	}
	for _, id := range []model.ID{"system", "client-a"} {
		usage, usageErr := manager.UsageContext(t.Context(), id)
		if usageErr != nil || usage.Used != 1000 || usage.Reserved != 0 {
			t.Fatal(id, usage, usageErr)
		}
	}
}

func TestPersistentCalendarBudgetResetsAtLocalBoundary(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "calendar-budgets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	configured := budget.Config{ID: "daily", Name: "Daily", Scope: budget.ScopeSystem, Limit: 10, Hard: true, Action: budget.ActionReject, Window: budget.WindowDaily, Timezone: "America/New_York"}
	manager, err := NewPersistent([]budget.Config{configured}, 10, store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 8, 4, 30, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	lease, err := manager.Reserve(t.Context(), []model.ID{"daily"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lease.Consume(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if err = lease.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Reserve(t.Context(), []model.ID{"daily"}, 1); !errors.Is(err, budget.ErrExceeded) {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if usage, usageErr := manager.Usage("daily"); usageErr != nil || usage != (budget.Usage{}) {
		t.Fatal(usage, usageErr)
	}
	lease, err = manager.Reserve(t.Context(), []model.ID{"daily"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lease.Consume(t.Context(), 4); err != nil {
		t.Fatal(err)
	}
	if err = lease.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewPersistent([]budget.Config{configured}, 10, store)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	if usage, usageErr := restarted.Usage("daily"); usageErr != nil || usage.Used != 4 || usage.Reserved != 0 {
		t.Fatal(usage, usageErr)
	}
}

func TestConcurrentReservationsCannotOverspend(t *testing.T) {
	m, _ := New([]budget.Config{{ID: "system", Name: "System", Limit: 1000, Hard: true, Action: budget.ActionReject}}, 10)
	var group sync.WaitGroup
	var granted int
	var lock sync.Mutex
	for range 200 {
		group.Go(func() {
			lease, err := m.Reserve(context.Background(), []model.ID{"system"}, 10)
			if err == nil {
				_, _ = lease.Consume(t.Context(), 10)
				_ = lease.Close(t.Context())
				lock.Lock()
				granted += 10
				lock.Unlock()
			}
		})
	}
	group.Wait()
	usage, _ := m.Usage("system")
	if granted != 1000 || usage.Used != 1000 || usage.Reserved != 0 {
		t.Fatal(granted, usage)
	}
}

func TestStatusesExposeCurrentWindowAndRemainingAllowance(t *testing.T) {
	configs := []budget.Config{
		{ID: "system", Name: "System", Limit: 100, Hard: true, Action: budget.ActionReject},
		{ID: "daily", Name: "Daily", Scope: budget.ScopeClient, ScopeID: "client", Limit: 10, Hard: true, Action: budget.ActionReject, Window: budget.WindowDaily, Timezone: "America/New_York"},
	}
	m, err := New(configs, 100)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC) }
	lease, err := m.Reserve(t.Context(), []model.ID{"daily"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lease.Consume(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	statuses, err := m.Statuses(t.Context())
	if err != nil || len(statuses) != 2 {
		t.Fatal(statuses, err)
	}
	daily, system := statuses[0], statuses[1]
	if daily.ID != "daily" || daily.Used != 7 || daily.Reserved != 3 || daily.Remaining != 0 || !daily.Exhausted || daily.WindowStart == nil || daily.WindowEnd == nil || daily.WindowEnd.Sub(*daily.WindowStart) != 23*time.Hour {
		t.Fatal(daily)
	}
	if system.ID != "system" || system.Scope != budget.ScopeSystem || system.Window != budget.WindowLifetime || system.Remaining != 100 || system.Exhausted || system.WindowStart != nil || system.WindowEnd != nil {
		t.Fatal(system)
	}
}
func TestLeaseNeverGrantsPastReservation(t *testing.T) {
	m, _ := New([]budget.Config{{ID: "system", Name: "System", Limit: 20, Hard: true, Action: budget.ActionReject}}, 20)
	lease, _ := m.Reserve(context.Background(), []model.ID{"system"}, 10)
	allowed, err := lease.Consume(t.Context(), 15)
	if !errors.Is(err, budget.ErrExceeded) || allowed != 10 {
		t.Fatal(allowed, err)
	}
	if err := lease.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Consume(t.Context(), 1); !errors.Is(err, budget.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := m.Reserve(context.Background(), []model.ID{"system"}, 11); !errors.Is(err, budget.ErrExceeded) {
		t.Fatal(err)
	}
	_ = traffic.Bytes(0)
}
