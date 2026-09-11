package sqlite

import (
	"errors"
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
	status, err := s.Status(t.Context())
	if err != nil || status.SchemaVersion != 4 || status.JournalMode != "wal" || status.EndpointCount != 1 {
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
	if _, err = s.db.Exec("INSERT INTO schema_migrations VALUES (5,'future','unknown')"); err != nil {
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
	if err != nil || status.SchemaVersion != 4 {
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
