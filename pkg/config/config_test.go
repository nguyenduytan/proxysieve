package config

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

func TestChainConfigurationReferencesAndIsolation(t *testing.T) {
	configured := Defaults(t.TempDir())
	configured.Proxies = []proxy.Endpoint{
		{ID: "first-proxy", Name: "First", Protocol: proxy.HTTP, Host: "first.example.invalid", Port: 8080, Enabled: true, TrustedRemoteDNS: true},
		{ID: "second-proxy", Name: "Second", Protocol: proxy.SOCKS5H, Host: "second.example.invalid", Port: 1080, Enabled: true, TrustedRemoteDNS: true},
	}
	configured.Pools = []routing.Pool{
		{ID: "first-pool", Name: "First", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"first-proxy"}, Enabled: true},
		{ID: "second-pool", Name: "Second", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"second-proxy"}, Enabled: true},
	}
	configured.Chains = []routing.Chain{{ID: "privacy-chain", Name: "Privacy chain", Hops: []routing.Hop{{PoolID: "first-pool"}, {PoolID: "second-pool"}}, Enabled: true}}
	configured.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "Default", Rules: []policy.Rule{{ID: "chain", Name: "Chain", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "chain", ChainID: "privacy-chain"}}}}}}
	if err := configured.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := configured.Clone()
	configured.Chains[0].Hops[0].PoolID = "mutated"
	if clone.Chains[0].Hops[0].PoolID != "first-pool" {
		t.Fatal("chain configuration aliased")
	}
	for name, mutate := range map[string]func(*Config){
		"missing chain":          func(c *Config) { c.Policies[0].Rules[0].Actions[0].ChainID = "missing" },
		"missing pool":           func(c *Config) { c.Chains[0].Hops[0].PoolID = "missing" },
		"overlapping membership": func(c *Config) { c.Pools[1].EndpointIDs[0] = "first-proxy" },
		"sticky hop":             func(c *Config) { c.Pools[0].SessionPolicy = publicsession.Policy{Strategy: publicsession.Client} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := clone.Clone()
			mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
}

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
