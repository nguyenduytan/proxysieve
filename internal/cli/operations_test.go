package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

func TestBackupAndRestoreDatabase(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "proxysieve.db")
	backup := filepath.Join(dir, "backup.db")
	repository, err := sqlite.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := contract.Endpoint("kept")
	if _, err = repository.Put(t.Context(), endpoint, 0); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	pool := contract.Pool("pool")
	pool.EndpointIDs = []model.ID{endpoint.ID}
	if _, err = repository.PutPool(t.Context(), pool, 0); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	document := contract.Policy("policy")
	if _, err = repository.PutPolicy(t.Context(), document, 0); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	bundle, err := repository.LoadRuntimeInventory(t.Context())
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	if _, err = repository.ActivateRuntime(t.Context(), bundle, 0, "operator", 7, base); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	if err = repository.RecordTraffic(t.Context(), traffic.Event{At: base.Add(time.Second), RequestID: "restored-request", ConnectionID: "restored-connection", PolicyID: document.ID, PoolID: pool.ID, ProxyID: endpoint.ID, Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 42}); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	if err = repository.Close(); err != nil {
		t.Fatal(err)
	}
	if err = backupDatabase(t.Context(), source, backup); err != nil {
		t.Fatal(err)
	}
	repository, err = sqlite.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Put(t.Context(), contract.Endpoint("discarded"), 0); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	_ = repository.Close()
	if err = restoreDatabase(t.Context(), source, backup); err != nil {
		t.Fatal(err)
	}
	repository, err = sqlite.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	if _, err = repository.Get(t.Context(), "kept"); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Get(t.Context(), "discarded"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("restore retained post-backup data: %v", err)
	}
	if _, err = repository.GetPool(t.Context(), pool.ID); err != nil {
		t.Fatalf("restored pool missing: %v", err)
	}
	if _, err = repository.GetPolicy(t.Context(), document.ID); err != nil {
		t.Fatalf("restored policy missing: %v", err)
	}
	runtime, err := repository.CurrentRuntime(t.Context())
	if err != nil || runtime.Revision != 1 || runtime.SourceRevision != 7 || len(runtime.Bundle.Proxies) != 1 || len(runtime.Bundle.Pools) != 1 || len(runtime.Bundle.Policies) != 1 {
		t.Fatalf("restored runtime mismatch: %+v %v", runtime, err)
	}
	summary, err := repository.TrafficSummary(t.Context(), sqlite.TrafficQuery{From: base, Until: base.Add(time.Minute)})
	if err != nil || summary.Totals.RequestCount != 1 || summary.Totals.UpstreamDownload != 42 {
		t.Fatalf("restored analytics mismatch: %+v %v", summary, err)
	}
	archives, err := filepath.Glob(source + ".pre-restore-*.db")
	if err != nil || len(archives) != 1 {
		t.Fatalf("expected one pre-restore archive, got %v %v", archives, err)
	}
	if info, err := os.Stat(archives[0]); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("pre-restore archive missing: %v", err)
	}
}

func TestDBCommands(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".proxysieve"), 0700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runDB([]string{"status", "--json"}, &stdout, &stderr, home, map[string]string{}); code != 0 || stderr.Len() != 0 {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var status sqlite.Status
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil || status.SchemaVersion < 1 || status.JournalMode != "wal" {
		t.Fatalf("invalid status: %+v %v", status, err)
	}
	for _, command := range []string{"migrate", "compact"} {
		stdout.Reset()
		stderr.Reset()
		if code := runDB([]string{command}, &stdout, &stderr, home, map[string]string{}); code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "schema") {
			t.Fatalf("%s code=%d stdout=%q stderr=%q", command, code, stdout.String(), stderr.String())
		}
	}
}

func TestRestoreRejectsInvalidBackup(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "invalid.db")
	if err := os.WriteFile(backup, []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreDatabase(t.Context(), filepath.Join(dir, "target.db"), backup); !errors.Is(err, store.ErrSchema) {
		t.Fatalf("invalid backup should fail schema validation: %v", err)
	}
}

func TestRestoreRejectsEmptySQLiteBackupWithoutMutatingIt(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(backup, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreDatabase(t.Context(), filepath.Join(dir, "target.db"), backup); !errors.Is(err, store.ErrSchema) {
		t.Fatalf("empty SQLite backup should fail schema validation: %v", err)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Size() != 0 {
		t.Fatalf("validator mutated backup: info=%v err=%v", info, err)
	}
}
