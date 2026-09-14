package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/configload"
	"github.com/nguyenduytan/proxysieve/pkg/config"
)

func TestPortableExportRoundTripAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	value := config.Defaults(filepath.Join(dir, "home"))
	archive := filepath.Join(dir, "proxysieve.export.zip")
	if err := writePortableExport(archive, value); err != nil {
		t.Fatal(err)
	}
	document, err := readPortableExport(archive)
	if err != nil {
		t.Fatal(err)
	}
	if document.Manifest.FormatVersion != portableFormatVersion || document.Manifest.Product != "ProxySieve" || document.Config.Validate() != nil {
		t.Fatalf("invalid document: %+v", document)
	}
	output := filepath.Join(dir, "imported.yaml")
	if err = writeImportedConfig(output, document.Config); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := configload.Load(configload.Options{Home: filepath.Join(dir, "other-home"), File: input, Env: map[string]string{}})
	_ = input.Close()
	if err != nil || loaded.Config.Server.DataDir != value.Server.DataDir || len(loaded.Config.Listeners) != len(value.Listeners) {
		t.Fatalf("imported config mismatch: err=%v config=%+v", err, loaded.Config)
	}
	if err = writePortableExport(archive, value); !errors.Is(err, errPortable) {
		t.Fatalf("overwrite was accepted: %v", err)
	}
}

func TestPortableExportRejectsTamperedManifestAndBounds(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "export.zip")
	if err := writePortableExport(archive, config.Defaults(dir)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 32 {
		t.Fatal("archive unexpectedly small")
	}
	// A byte-level mutation must fail validation rather than being accepted as a
	// different product/format.
	b[len(b)-1] ^= 1
	if err = os.WriteFile(archive, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readPortableExport(archive); !errors.Is(err, errPortable) {
		t.Fatalf("tampered archive accepted: %v", err)
	}
}

func TestPortableCommandDryRun(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "export.zip")
	if err := writePortableExport(archive, config.Defaults(dir)); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runExport([]string{"import", "--path", archive, "--dry-run"}, &stdout, &stderr, dir, map[string]string{}); code != 0 || !strings.Contains(stdout.String(), "portable export is valid") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunExportCommand(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "export.zip")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"export", "--path", archive}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := readPortableExport(archive); err != nil {
		t.Fatal(err)
	}
}
