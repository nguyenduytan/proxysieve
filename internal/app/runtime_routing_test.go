package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	internalhealth "github.com/nguyenduytan/proxysieve/internal/health"
	"github.com/nguyenduytan/proxysieve/internal/secrets"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalshadow "github.com/nguyenduytan/proxysieve/internal/shadow"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestRuntimeActivationIsAtomicRevisionedAndRestartSafe(t *testing.T) {
	base := config.Defaults(t.TempDir())
	base.Listeners = base.Listeners[:1]
	configured := store.RuntimeBundle{Proxies: base.Proxies, Pools: base.Pools, Policies: base.Policies}
	if err := os.MkdirAll(base.Server.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	repository, err := sqlite.Open(t.Context(), filepath.Join(base.Server.DataDir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	runtime, err := newRoutingRuntime(base, configured, store.RuntimeRecord{Revision: 0, Bundle: configured})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC)
	control := &runtimeControl{runtime: runtime, store: repository, now: func() time.Time { return now }}
	endpoint := contract.Endpoint("proxy")
	endpoint.TrustedRemoteDNS = true
	if _, err = repository.Put(t.Context(), endpoint, 0); err != nil {
		t.Fatal(err)
	}
	pool := routing.Pool{ID: "pool", Name: "Pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, SessionPolicy: publicsession.Policy{Strategy: publicsession.Client, TTL: time.Hour, IdleTTL: time.Minute, MaxRequests: 100, MaxBytes: 1 << 20}, Enabled: true}
	if _, err = repository.PutPool(t.Context(), pool, 0); err != nil {
		t.Fatal(err)
	}
	document := policy.Policy{Version: 1, ID: "default", Name: "Proxy", Rules: []policy.Rule{{ID: "route", Name: "Route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}
	if _, err = repository.PutPolicy(t.Context(), document, 0); err != nil {
		t.Fatal(err)
	}
	first, err := control.ActivateRuntime(t.Context(), 0, "admin")
	if err != nil || first.Revision != 1 {
		t.Fatal(first, err)
	}
	if first.Bundle.Pools[0].SessionPolicy != pool.SessionPolicy {
		t.Fatal("activation lost pool session policy", first.Bundle.Pools[0])
	}
	if staged, stagedErr := control.HasStagedRuntimeChanges(t.Context()); stagedErr != nil || staged {
		t.Fatalf("activated inventory should be synchronized: staged=%v err=%v", staged, stagedErr)
	}
	health, _ := internalhealth.New(publichealth.Defaults(), nil)
	router := &router{policyID: "default", runtime: runtime, resolver: resolver{}, destination: security.DestinationPolicy{DenyPrivate: true}, credentials: secrets.Environment{}, health: health, directPolicy: base.Security}
	request := policy.RequestContext{Host: "origin.example.invalid", Port: 80, Timestamp: now}
	inFlight, err := router.Evaluate(t.Context(), request, policy.Visibility{Host: true})
	if err != nil || inFlight.RuntimeRevision != 1 || inFlight.Actions[0].Type != "proxy" {
		t.Fatal(inFlight, err)
	}
	document.Name = "Reject"
	document.Rules[0].Actions = []policy.Action{{Type: "reject"}}
	if _, err = repository.PutPolicy(t.Context(), document, 1); err != nil {
		t.Fatal(err)
	}
	if staged, stagedErr := control.HasStagedRuntimeChanges(t.Context()); stagedErr != nil || !staged {
		t.Fatalf("updated inventory should be staged: staged=%v err=%v", staged, stagedErr)
	}
	second, err := control.ActivateRuntime(t.Context(), 1, "admin")
	if err != nil || second.Revision != 2 {
		t.Fatal(second, err)
	}
	if staged, stagedErr := control.HasStagedRuntimeChanges(t.Context()); stagedErr != nil || staged {
		t.Fatalf("second activation should synchronize inventory: staged=%v err=%v", staged, stagedErr)
	}
	if route, routeErr := router.Route(t.Context(), request, inFlight); routeErr != nil || route.Action != "proxy" {
		t.Fatalf("in-flight request lost revision 1: route=%+v err=%v", route, routeErr)
	}
	current, err := router.Evaluate(t.Context(), request, policy.Visibility{Host: true})
	if err != nil || current.RuntimeRevision != 2 || current.Actions[0].Type != "reject" {
		t.Fatal(current, err)
	}
	if err = repository.DeletePolicy(t.Context(), "default", 2); err != nil {
		t.Fatal(err)
	}
	if staged, stagedErr := control.HasStagedRuntimeChanges(t.Context()); stagedErr != nil || !staged {
		t.Fatalf("deleted inventory should be staged: staged=%v err=%v", staged, stagedErr)
	}
	if _, err = control.ActivateRuntime(t.Context(), 2, "admin"); !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("invalid candidate should fail closed: %v", err)
	}
	if runtime.currentRecord().Revision != 2 {
		t.Fatal("failed activation changed the last-known-good revision")
	}
	rolledBack, err := control.RollbackRuntime(t.Context(), 2, 1, "admin")
	if err != nil || rolledBack.Revision != 3 || rolledBack.SourceRevision != 1 {
		t.Fatal(rolledBack, err)
	}
	if staged, stagedErr := control.HasStagedRuntimeChanges(t.Context()); stagedErr != nil || !staged {
		t.Fatalf("rollback snapshot should differ from saved inventory: staged=%v err=%v", staged, stagedErr)
	}
	reloadedRecord, err := runtimeRecordOrConfig(t.Context(), repository, configured)
	if err != nil || reloadedRecord.Revision != 3 {
		t.Fatal(reloadedRecord, err)
	}
	reloaded, err := newRoutingRuntime(base, reloadedRecord.Bundle, reloadedRecord)
	if err != nil || reloaded.currentRecord().Bundle.Policies[0].Name != "Proxy" || reloaded.currentRecord().Bundle.Pools[0].SessionPolicy != pool.SessionPolicy {
		t.Fatal(reloadedRecord, err)
	}
}

func TestRuntimeCompilationValidatesBundleChains(t *testing.T) {
	base := config.Defaults(t.TempDir())
	bundle := store.RuntimeBundle{
		Chains: []routing.Chain{{
			ID: "invalid-chain", Name: "Invalid chain",
			Hops: []routing.Hop{{PoolID: "missing-one"}, {PoolID: "missing-two"}}, Enabled: true,
		}},
	}
	if _, err := newRoutingRuntime(base, bundle, store.RuntimeRecord{Bundle: bundle}); !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("expected invalid bundle chain to fail compilation, got %v", err)
	}
}

func TestShadowPolicyNeverChangesLiveDecision(t *testing.T) {
	base := config.Defaults(t.TempDir())
	for i := range base.Listeners {
		base.Listeners[i].Policy = "active"
	}
	active := policy.Policy{Version: 1, ID: "active", Name: "Active", Rules: []policy.Rule{{ID: "route", Name: "Route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "direct"}}}}}
	bundle := store.RuntimeBundle{Policies: []policy.Policy{active}}
	runtime, err := newRoutingRuntime(base, bundle, store.RuntimeRecord{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	config := publicshadow.Config{ID: "candidate", Name: "Candidate", ActivePolicyID: active.ID, Enabled: true, Policy: policy.Policy{Version: 1, ID: "candidate-policy", Name: "Candidate", Rules: []policy.Rule{{ID: "route", Name: "Route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "block"}}}}}}
	manager, err := internalshadow.New([]store.ShadowRecord{{Shadow: config, Revision: 1}})
	if err != nil {
		t.Fatal(err)
	}
	router := &router{policyID: active.ID, runtime: runtime, shadows: manager}
	result, err := router.Evaluate(t.Context(), policy.RequestContext{}, policy.Visibility{})
	if err != nil || len(result.Actions) != 1 || result.Actions[0].Type != "direct" {
		t.Fatalf("live result changed: %+v %v", result, err)
	}
	comparison, ok := manager.Comparison(config.ID)
	if !ok || comparison.DifferentDecisions != 1 || comparison.ShadowDecisions["block"] != 1 {
		t.Fatalf("comparison=%+v", comparison)
	}
}

func TestPoolSelectionUsesHealthScoreAndLatency(t *testing.T) {
	base := config.Defaults(t.TempDir())
	endpoints := []proxy.Endpoint{
		{ID: "fast", Name: "Fast", Protocol: proxy.HTTP, Host: "fast.example.invalid", Port: 8080, Enabled: true},
		{ID: "slow", Name: "Slow", Protocol: proxy.HTTP, Host: "slow.example.invalid", Port: 8080, Enabled: true},
		{ID: "degraded", Name: "Degraded", Protocol: proxy.HTTP, Host: "degraded.example.invalid", Port: 8080, Enabled: true},
	}
	pool := routing.Pool{ID: "pool", Name: "Pool", Strategy: routing.HighestHealth, EndpointIDs: []model.ID{"fast", "slow", "degraded"}, MinHealthScore: 60, MaxLatency: 50 * time.Millisecond, Enabled: true}
	bundle := store.RuntimeBundle{Proxies: endpoints, Pools: []routing.Pool{pool}, Policies: base.Policies}
	runtime, err := newRoutingRuntime(base, bundle, store.RuntimeRecord{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	health, _ := internalhealth.New(publichealth.Defaults(), nil)
	for range 4 {
		_, _ = health.Observe("fast", publichealth.Observation{Success: true, Latency: 10 * time.Millisecond})
		_, _ = health.Observe("slow", publichealth.Observation{Success: true, Latency: 100 * time.Millisecond})
	}
	_, _ = health.Observe("degraded", publichealth.Observation{Success: false, Latency: 10 * time.Millisecond})
	router := &router{health: health}
	selected, err := router.selectEndpoint(t.Context(), "pool", policy.RequestContext{}, runtime.currentSnapshot())
	if err != nil || selected.ID != "fast" {
		t.Fatal(selected.ID, err)
	}

	pool.EndpointIDs = []model.ID{"unknown"}
	pool.MinHealthScore = 100
	unknown := proxy.Endpoint{ID: "unknown", Name: "Unknown", Protocol: proxy.HTTP, Host: "unknown.example.invalid", Port: 8080, Enabled: true}
	bundle = store.RuntimeBundle{Proxies: []proxy.Endpoint{unknown}, Pools: []routing.Pool{pool}, Policies: base.Policies}
	runtime, err = newRoutingRuntime(base, bundle, store.RuntimeRecord{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	selected, err = router.selectEndpoint(t.Context(), "pool", policy.RequestContext{}, runtime.currentSnapshot())
	if err != nil || selected.ID != "unknown" {
		t.Fatal("unknown endpoint was not allowed to collect initial health", selected.ID, err)
	}
}
