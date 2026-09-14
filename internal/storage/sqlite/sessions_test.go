package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internalsession "github.com/nguyenduytan/proxysieve/internal/session"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

func TestDurableSessionsSurviveSQLiteReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("a durable sqlite session key long enough")
	manager, err := internalsession.NewPersistent(t.Context(), key, 10, nil, store)
	if err != nil {
		t.Fatal(err)
	}
	request := internalsession.Request{ClientID: "client", PoolID: "pool", Key: "raw-key", RuntimeRevision: 1, Policy: publicsession.Policy{Strategy: publicsession.Explicit, TTL: time.Hour}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }}
	created, err := manager.Resolve(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.RecordUsage(t.Context(), created.Session.ID, 2, 3); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	restarted, err := internalsession.NewPersistent(t.Context(), key, 10, nil, store)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := restarted.Get(t.Context(), created.Session.ID)
	if err != nil || entry.KeyHash == "raw-key" || entry.RequestCount != 1 || entry.UploadBytes != 2 || entry.DownloadBytes != 3 {
		t.Fatalf("unexpected restored session: %+v %v", entry, err)
	}
}

func TestDurableSessionBatchRollsBack(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	entry := publicsession.Session{ID: "first", ClientID: "client", KeyHash: strings.Repeat("a", 64), PoolID: "pool", ProxyEndpointID: "proxy", CreatedAt: now, LastUsedAt: now, Status: "active", RotationReason: publicsession.Created, Policy: publicsession.Policy{Strategy: publicsession.Explicit}, RuntimeRevision: 1}
	duplicate := entry
	duplicate.ID = "second"
	if err = store.ApplySessions(t.Context(), publicsession.ChangeSet{Upserts: []publicsession.Session{entry, duplicate}}); err == nil {
		t.Fatal("duplicate active session key was accepted")
	}
	entries, err := store.LoadSessions(t.Context())
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed batch partially committed: %+v %v", entries, err)
	}
}
