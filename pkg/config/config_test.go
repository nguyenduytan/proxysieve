package config

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
)

func TestBudgetConfigurationScopes(t *testing.T) {
	c := Defaults(t.TempDir())
	c.Budgets = []publicbudget.Config{{ID: "system", Name: "System", Scope: publicbudget.ScopeSystem, Limit: 1024, Hard: true, Action: publicbudget.ActionReject}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot := c.Clone()
	c.Budgets[0].Name = "changed"
	if snapshot.Budgets[0].Name != "System" {
		t.Fatal("budget config aliased")
	}
	for _, configured := range []publicbudget.Config{
		{ID: "pool-limit", Name: "Missing pool", Scope: publicbudget.ScopePool, ScopeID: "missing", Limit: 1, Hard: true, Action: publicbudget.ActionReject},
		{ID: "bad-soft", Name: "Bad hard action", Scope: publicbudget.ScopeSystem, Limit: 1, Hard: true, Action: publicbudget.ActionAlert},
	} {
		invalid := Defaults(t.TempDir())
		invalid.Budgets = []publicbudget.Config{configured}
		if err := invalid.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatal(configured, err)
		}
	}
}

func TestSafeDefaultsAndManager(t *testing.T) {
	c := Defaults(t.TempDir())
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Inspect.Enabled || c.Security.AllowDirect || !c.Security.DenyPrivate || c.Cache.Response.Enabled || c.Logging.CaptureBodies {
		t.Fatal("unsafe defaults")
	}
	if c.Traffic.RetentionDays != 30 || c.Traffic.MinuteRetentionDays != 90 || c.Traffic.HourRetentionDays != 365 || c.Traffic.DayRetentionDays != 3650 {
		t.Fatal("unexpected traffic retention defaults", c.Traffic)
	}
	m, err := NewManager(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Listeners[0].Bind = "0.0.0.0:8080"
	if m.Snapshot().Config.Listeners[0].Bind != "127.0.0.1:8080" {
		t.Fatal("input aliased")
	}
	bad := m.Snapshot()
	bad.Config.Version = 2
	if _, err = m.Apply(1, bad.Config); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if m.Snapshot().Revision != 1 {
		t.Fatal("invalid config published")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			snap := m.Snapshot()
			snap.Config.Logging.Level = "debug"
			_, err := m.Apply(1, snap.Config)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 || m.Snapshot().Revision != 2 {
		t.Fatal("stale writers accepted")
	}
	snap := m.Snapshot()
	snap.Config.Listeners[0].Bind = "mutated"
	if m.Snapshot().Config.Listeners[0].Bind == "mutated" {
		t.Fatal("snapshot aliased")
	}
}
