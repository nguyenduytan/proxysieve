package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestBackupAndRestoreDatabase(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "proxysieve.db")
	backup := filepath.Join(dir, "backup.db")
	repository, err := sqlite.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Put(t.Context(), contract.Endpoint("kept"), 0); err != nil {
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
	archives, err := filepath.Glob(source + ".pre-restore-*.db")
	if err != nil || len(archives) != 1 {
		t.Fatalf("expected one pre-restore archive, got %v %v", archives, err)
	}
	if info, err := os.Stat(archives[0]); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("pre-restore archive missing: %v", err)
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
