package sqlite

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestContract(t *testing.T) {
	contract.Run(t, func(t *testing.T) store.EndpointStore {
		t.Helper()
		s, err := Open(t.Context(), filepath.Join(t.TempDir(), "store.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
func TestSourceContract(t *testing.T) {
	contract.RunSources(t, func(t *testing.T) store.Sources {
		t.Helper()
		repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "sources.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = repository.Close() })
		return repository
	})
}
func TestPoolContract(t *testing.T) {
	contract.RunPools(t, func(t *testing.T) store.Pools {
		t.Helper()
		repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "pools.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = repository.Close() })
		return repository
	})
}
func TestChainContract(t *testing.T) {
	contract.RunChains(t, func(t *testing.T) store.Chains {
		t.Helper()
		repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "chains.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = repository.Close() })
		return repository
	})
}
func TestPolicyContract(t *testing.T) {
	contract.RunPolicies(t, func(t *testing.T) store.Policies {
		t.Helper()
		repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "policies.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = repository.Close() })
		return repository
	})
}

func TestInventoryTransactionRollsBackEndpointsAndSources(t *testing.T) {
	repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "inventory-transaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	endpoint := contract.Endpoint("endpoint")
	source := contract.Source("source")
	if _, err = repository.Put(t.Context(), endpoint, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.PutSource(t.Context(), source, 0); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("force rollback")
	err = repository.WithinInventoryTransaction(t.Context(), func(endpoints store.Endpoints, sources store.Sources) error {
		endpoint.Name = "changed"
		if _, putErr := endpoints.Put(t.Context(), endpoint, 1); putErr != nil {
			return putErr
		}
		source.LastRefreshStatus = "changed"
		if _, putErr := sources.PutSource(t.Context(), source, 1); putErr != nil {
			return putErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	storedEndpoint, endpointErr := repository.Get(t.Context(), endpoint.ID)
	storedSource, sourceErr := repository.GetSource(t.Context(), source.ID)
	if endpointErr != nil || sourceErr != nil || storedEndpoint.Revision != 1 || storedEndpoint.Endpoint.Name == "changed" || storedSource.Revision != 1 || storedSource.Source.LastRefreshStatus != "" {
		t.Fatal(storedEndpoint, storedSource, endpointErr, sourceErr)
	}
	if err = repository.WithinInventoryTransaction(t.Context(), nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestPersistenceAndMigrationChecksums(t *testing.T) {
	// '#' and spaces exercise SQLite URI escaping on every supported OS.
	path := filepath.Join(t.TempDir(), "store with spaces #.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(t.Context(), contract.Endpoint("a"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutSource(t.Context(), contract.Source("source"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPool(t.Context(), contract.Pool("pool"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPolicy(t.Context(), contract.Policy("policy"), 0); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(t.Context())
	if err != nil || status.SchemaVersion != 22 || status.JournalMode != "wal" || status.EndpointCount != 1 || status.SourceCount != 1 || status.PoolCount != 1 || status.PolicyCount != 1 {
		t.Fatalf("%+v %v", status, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetSource(t.Context(), "source"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetPool(t.Context(), "pool"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetPolicy(t.Context(), "policy"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE schema_migrations SET checksum='tampered'"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if s, err = Open(t.Context(), path); !errors.Is(err, store.ErrSchema) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal(err)
	}
}
func TestFutureSchemaRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO schema_migrations VALUES (23,'future','unknown')"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if s, err = Open(t.Context(), path); !errors.Is(err, store.ErrSchema) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal(err)
	}
}
func TestSchemaTwelveUpgradePreservesInventoryAndAddsPolicies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema-ten.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := &Store{db: database, endpoints: endpoints{q: database}}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil || len(names) != 22 {
		t.Fatal(names, err)
	}
	files := fstest.MapFS{}
	for _, name := range names[:12] {
		data, readErr := migrations.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if err = old.migrate(t.Context(), files); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Put(t.Context(), contract.Endpoint("preserved"), 0); err != nil {
		t.Fatal(err)
	}
	old.sources = sources{q: database}
	if _, err = old.PutSource(t.Context(), contract.Source("source"), 0); err != nil {
		t.Fatal(err)
	}
	old.pools = pools{q: database}
	if _, err = old.PutPool(t.Context(), contract.Pool("pool"), 0); err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	if _, err = repository.Get(t.Context(), "preserved"); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.GetSource(t.Context(), "source"); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.GetPool(t.Context(), "pool"); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.PutPolicy(t.Context(), contract.Policy("policy"), 0); err != nil {
		t.Fatal(err)
	}
	status, err := repository.Status(t.Context())
	if err != nil || status.SchemaVersion != 22 || status.EndpointCount != 1 || status.SourceCount != 1 || status.PoolCount != 1 || status.PolicyCount != 1 {
		t.Fatal(status, err)
	}
}

func TestSchemaEighteenUpgradePreservesLifetimeBudgetUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema-18.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := &Store{db: database}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil || len(names) != 22 {
		t.Fatal(names, err)
	}
	files := fstest.MapFS{}
	for _, name := range names[:18] {
		data, readErr := migrations.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if err = old.migrate(t.Context(), files); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO budget_usage(budget_id,used_bytes,reserved_bytes) VALUES ('system',7,3)"); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	repository, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	var windowStart, used, reserved int64
	if err = repository.db.QueryRow("SELECT window_start,used_bytes,reserved_bytes FROM budget_usage WHERE budget_id='system'").Scan(&windowStart, &used, &reserved); err != nil || windowStart != 0 || used != 7 || reserved != 3 {
		t.Fatal(windowStart, used, reserved, err)
	}
}

func TestMigrationAtomicity(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	b, err := migrations.ReadFile("migrations/000001_endpoints.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{
		"migrations/000001_endpoints.sql": {Data: b},
		"migrations/000002_broken.sql":    {Data: []byte("CREATE TABLE should_rollback(id TEXT); INVALID SQL;")},
	}
	if err = s.migrate(t.Context(), files); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var n int
	if err = s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='should_rollback'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("table survived rollback: %d %v", n, err)
	}
	status, err := s.Status(t.Context())
	if err != nil || status.SchemaVersion != 22 {
		t.Fatalf("%+v %v", status, err)
	}
}
func TestBadPathsAndClosedStore(t *testing.T) {
	for _, path := range []string{"", ":memory:", "file:unsafe?mode=memory", t.TempDir()} {
		if s, err := Open(t.Context(), path); err == nil {
			_ = s.Close()
			t.Fatalf("accepted %q", path)
		}
	}
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(t.Context(), path); err == nil {
		_ = s.Close()
		t.Fatal("accepted corrupt db")
	}
}
