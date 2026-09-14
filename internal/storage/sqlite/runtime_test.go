package sqlite

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestRuntimeSnapshotsPersistConflictAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	repository, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	bundle := store.RuntimeBundle{
		Proxies:  []proxy.Endpoint{contract.Endpoint("proxy")},
		Pools:    []routing.Pool{{ID: "pool", Name: "Pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, Enabled: true}},
		Policies: []policy.Policy{{Version: 1, ID: "default", Name: "Default", Rules: []policy.Rule{{ID: "deny", Name: "Deny", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "reject"}}}}}},
	}
	firstAt := time.Date(2026, 9, 14, 1, 2, 3, 4, time.UTC)
	first, err := repository.ActivateRuntime(t.Context(), bundle, 0, "admin", 0, firstAt)
	if err != nil || first.Revision != 1 || !first.ActivatedAt.Equal(firstAt) {
		t.Fatal(first, err)
	}
	if _, err = repository.ActivateRuntime(t.Context(), bundle, 0, "admin", 0, firstAt); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale activation should conflict: %v", err)
	}
	bundle.Policies[0].Name = "Updated"
	second, err := repository.ActivateRuntime(t.Context(), bundle, 1, "operator", 0, firstAt.Add(time.Minute))
	if err != nil || second.Revision != 2 {
		t.Fatal(second, err)
	}
	history, err := repository.ListRuntime(t.Context(), 0, 10)
	if err != nil || len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 1 {
		t.Fatal(history, err)
	}
	old, err := repository.GetRuntime(t.Context(), 1)
	if err != nil || old.Bundle.Policies[0].Name != "Default" {
		t.Fatal(old, err)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	current, err := repository.CurrentRuntime(t.Context())
	if err != nil || current.Revision != 2 || current.Bundle.Policies[0].Name != "Updated" {
		t.Fatal(current, err)
	}
	rolledBack, err := repository.ActivateRuntime(t.Context(), old.Bundle, 2, "admin", old.Revision, firstAt.Add(2*time.Minute))
	if err != nil || rolledBack.Revision != 3 || rolledBack.SourceRevision != 1 || rolledBack.Bundle.Policies[0].Name != "Default" {
		t.Fatal(rolledBack, err)
	}
}

func TestRuntimeSnapshotsRetainLatestHundredRevisions(t *testing.T) {
	repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	bundle := store.RuntimeBundle{}
	stamp := time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC)
	for expected := int64(0); expected < 102; expected++ {
		if _, err = repository.ActivateRuntime(t.Context(), bundle, expected, "admin", 0, stamp.Add(time.Duration(expected)*time.Second)); err != nil {
			t.Fatalf("activate revision %d: %v", expected+1, err)
		}
	}
	history, err := repository.ListRuntime(t.Context(), 0, 100)
	if err != nil || len(history) != 100 {
		t.Fatalf("unexpected retained history count: count=%d err=%v", len(history), err)
	}
	if history[0].Revision != 102 || history[99].Revision != 3 {
		t.Fatalf("unexpected retained history range: first=%d last=%d", history[0].Revision, history[99].Revision)
	}
	if _, err = repository.GetRuntime(t.Context(), 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired runtime revision should be removed: %v", err)
	}
}
