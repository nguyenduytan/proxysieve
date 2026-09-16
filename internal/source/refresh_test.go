package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestFileRefreshUsesMappedFormatAndPreservesInventoryOnFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "imports")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proxies.json"), []byte(`{"rows":[{"address":"proxy.example.invalid","listen":8080}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	source := contract.Source("file-source")
	source.Type = proxy.FileSource
	source.Config = map[string]string{
		"path": "proxies.json", "format": "json", "items_field": "rows",
		"host_field": "address", "port_field": "listen",
	}
	if _, err = repository.PutSource(t.Context(), source, 0); err != nil {
		t.Fatal(err)
	}
	refresher := NewRefresher(root)
	result, err := refresher.Refresh(t.Context(), RefreshRequest{
		ID: source.ID, Revision: 1, Store: repository, Now: time.Now().UTC(),
	})
	if err != nil || result.Created != 1 || result.Source.Revision != 2 {
		t.Fatal(result, err)
	}
	source = result.Source.Source
	source.Config["path"] = "../outside.json"
	updated, err := repository.PutSource(t.Context(), source, result.Source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := refresher.Refresh(t.Context(), RefreshRequest{
		ID: source.ID, Revision: updated.Revision, Store: repository, Now: time.Now().UTC(),
	})
	if !errors.Is(err, ErrConfig) || !failed.FailureRecorded {
		t.Fatal(failed, err)
	}
	endpoints, err := repository.List(t.Context(), store.Page{Limit: 10})
	if err != nil || len(endpoints) != 1 || endpoints[0].Endpoint.Host != "proxy.example.invalid" {
		t.Fatal(endpoints, err)
	}
}

func TestReadImportFileRejectsOutsideSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "imports")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.txt")
	if err := os.WriteFile(outside, []byte("proxy.example.invalid:8080"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "outside.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readImportFile(t.Context(), root, "outside.txt"); !errors.Is(err, ErrRead) {
		t.Fatal("outside symlink accepted", err)
	}
}

func TestReadImportFileRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(MaxBodyBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = readImportFile(t.Context(), root, "large.txt"); !errors.Is(err, ErrRead) {
		t.Fatal("oversized file accepted", err)
	}
}
