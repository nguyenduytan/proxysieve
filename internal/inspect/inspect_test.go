package inspect

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateOpenExportAndRotate(t *testing.T) {
	dataDir := t.TempDir()
	created, err := Create(dataDir, false)
	if err != nil || created.Fingerprint == "" || created.NotAfter.IsZero() {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err = Create(dataDir, false); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate create: %v", err)
	}
	manager, err := Open(dataDir, []string{"*.example.invalid"}, []string{"private.example.invalid"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, selected, err := manager.TLSConfig("private.example.invalid"); err != nil || selected {
		t.Fatalf("excluded host selected: %v %v", selected, err)
	}
	configuration, selected, err := manager.TLSConfig("api.example.invalid")
	if err != nil || !selected || len(configuration.Certificates) != 1 {
		t.Fatalf("included host: %+v %v %v", configuration, selected, err)
	}
	block, _ := pem.Decode(manager.CertificatePEM())
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := x509.ParseCertificates(configuration.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, err = leaves[0].Verify(x509.VerifyOptions{DNSName: "api.example.invalid", Roots: roots}); err != nil {
		t.Fatal(err)
	}
	exportPath := filepath.Join(t.TempDir(), "proxysieve-ca.crt")
	if err = Export(dataDir, exportPath); err != nil {
		t.Fatal(err)
	}
	if exported, err := os.ReadFile(exportPath); err != nil || string(exported) != string(created.Certificate) {
		t.Fatalf("export mismatch: %v", err)
	}
	rotated, err := Create(dataDir, true)
	if err != nil || rotated.Fingerprint == created.Fingerprint {
		t.Fatalf("rotate: %+v %v", rotated, err)
	}
	if _, err = Open(dataDir, []string{"api.example.invalid"}, nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestRecordsBoundedRedactedMetadata(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := Create(dataDir, false); err != nil {
		t.Fatal(err)
	}
	manager, err := Open(dataDir, []string{"example.invalid"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	manager.Record("GET", "https://example.invalid/private?token=fake", 200, http.Header{"Authorization": {"Bearer fake"}, "X-Trace": {"visible"}}, http.Header{"Content-Type": {"text/plain"}, "Set-Cookie": {"fake=secret"}}, 12, 34)
	recent := manager.Recent(1)
	if len(recent) != 1 || recent[0].URL != "https://example.invalid/private?redacted" || recent[0].RequestHeaders.Get("Authorization") != "[REDACTED]" || recent[0].RequestHeaders.Get("X-Trace") != "visible" || recent[0].ResponseHeaders.Get("Set-Cookie") != "[REDACTED]" || recent[0].ContentType != "text/plain" {
		t.Fatalf("unexpected observation: %+v", recent)
	}
}

func TestRejectsCorruptBundle(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := Create(dataDir, false); err != nil {
		t.Fatal(err)
	}
	_, target, _ := paths(dataDir)
	if err := os.WriteFile(target, []byte("not a CA"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInfo(dataDir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt bundle accepted: %v", err)
	}
}

func TestSecureFilePermissions(t *testing.T) {
	path, err := writeTemporary(t.TempDir(), []byte("private"))
	if err != nil {
		t.Fatalf("secure file: %T %v", err, err)
	}
	defer func() { _ = os.Remove(path) }()
	if err := verifySecureFile(path); err != nil {
		t.Fatalf("verify file: %T %v", err, err)
	}
}
