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
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
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
	pool := routing.Pool{ID: "pool", Name: "Pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, Enabled: true}
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
	if err != nil || reloaded.currentRecord().Bundle.Policies[0].Name != "Proxy" {
		t.Fatal(reloadedRecord, err)
	}
}
