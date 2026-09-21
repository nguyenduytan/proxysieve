package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorChecksDataDirectory(t *testing.T) {
	dir := t.TempDir()
	check := checkDataDir(dir)
	if check.Status != "pass" || !strings.Contains(check.Detail, "writable") {
		t.Fatalf("unexpected writable directory check: %+v", check)
	}
	missing := checkDataDir(filepath.Join(dir, "missing"))
	if missing.Status != "warn" {
		t.Fatalf("unexpected missing directory check: %+v", missing)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if check := checkDataDir(file); check.Status != "fail" {
		t.Fatalf("file path should fail data directory check: %+v", check)
	}
}

func TestDoctorChecksDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxysieve.db")
	if check := checkDatabase(context.Background(), path); check.Status != "warn" {
		t.Fatalf("missing database should warn: %+v", check)
	}
	databaseFile, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := databaseFile.Close(); err != nil {
		t.Fatal(err)
	}
	if check := checkDatabase(context.Background(), path); check.Status != "pass" || !strings.Contains(check.Detail, "schema 21") {
		t.Fatalf("database schema check failed: %+v", check)
	}
	if check := checkDatabase(context.Background(), "file:unsafe"); check.Status != "fail" {
		t.Fatalf("unsafe database path should fail: %+v", check)
	}
}

func TestDoctorChecksBindAvailability(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bind := listener.Addr().String()
	if check := checkBind("test", bind); check.Status != "fail" {
		t.Fatalf("occupied bind should fail: %+v", check)
	}
	_ = listener.Close()
	if check := checkBind("test", bind); check.Status != "pass" {
		t.Fatalf("released bind should pass: %+v", check)
	}
}

func TestDoctorReportJSON(t *testing.T) {
	report := doctorReport{Checks: []doctorCheck{{Name: "config", Status: "pass", Detail: "ok"}}}
	var out, errOut strings.Builder
	if code := writeDoctor(&errOut, &out, true, report, false); code != 0 || errOut.Len() != 0 {
		t.Fatalf("doctor JSON output failed: code=%d stderr=%q", code, errOut.String())
	}
	var decoded doctorReport
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil || len(decoded.Checks) != 1 {
		t.Fatalf("invalid doctor JSON: %q (%v)", out.String(), err)
	}
}

func TestDoctorTestConfigRemainsValid(t *testing.T) {
	c := doctorTestConfig(t.TempDir())
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
