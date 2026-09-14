package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestBackupToCreatesConsistentCopyAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	backup := filepath.Join(dir, "backups", "source.db")
	repository, err := Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	if _, err = repository.Put(t.Context(), contract.Endpoint("backup-endpoint"), 0); err != nil {
		t.Fatal(err)
	}
	if err = repository.BackupTo(t.Context(), backup); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("missing parent should fail safely: %v", err)
	}
	if err = os.Mkdir(filepath.Dir(backup), 0700); err != nil {
		t.Fatal(err)
	}
	if err = repository.BackupTo(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	copyStore, err := Open(t.Context(), backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copyStore.Get(t.Context(), "backup-endpoint"); err != nil {
		_ = copyStore.Close()
		t.Fatal(err)
	}
	_ = copyStore.Close()
	if err = repository.BackupTo(t.Context(), backup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("existing destination should be protected: %v", err)
	}
}
