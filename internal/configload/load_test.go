package configload

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrecedence(t *testing.T) {
	base := Options{Home: t.TempDir(), File: strings.NewReader("version: 1\nlogging:\n  level: debug\n"), Env: map[string]string{"PROXYSIEVE_LOG_LEVEL": "warn"}, Flags: map[string]string{"logging.level": "error"}, Runtime: map[string]string{"logging.level": "trace"}}
	e, err := Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if e.Config.Logging.Level != "trace" || e.Sources["logging.level"] != "runtime" {
		t.Fatalf("%+v", e)
	}
	base.File = strings.NewReader("version: 1\nlogging:\n  level: debug\n")
	base.Runtime = nil
	e, err = Load(base)
	if err != nil || e.Config.Logging.Level != "error" || e.Sources["logging.level"] != "flags" {
		t.Fatal(e, err)
	}
	base.File = strings.NewReader("version: 1\nlogging:\n  level: debug\n")
	base.Flags = nil
	e, err = Load(base)
	if err != nil || e.Config.Logging.Level != "warn" || e.Sources["logging.level"] != "environment" {
		t.Fatal(e, err)
	}
	base.File = strings.NewReader("version: 1\nlogging:\n  level: debug\n")
	base.Env = nil
	e, err = Load(base)
	if err != nil || e.Config.Logging.Level != "debug" || e.Sources["logging.level"] != "file" {
		t.Fatal(e, err)
	}
	base.File = nil
	e, err = Load(base)
	if err != nil || e.Config.Logging.Level != "info" || e.Sources["logging.level"] != "default" {
		t.Fatal(e, err)
	}
}
func TestInvalidDocuments(t *testing.T) {
	for _, doc := range []string{
		"", "logging: {}", "version: 2", "version: 1\nunknown: true", "version: 1\nversion: 1",
		"version: 1\nlogging:\n  password: fake-secret", "version: 1\nlogging: null", "version: 1\n---\nversion: 1",
		"version: 1\nlogging: &a {level: debug}\nadmin: *a", "version: 1\nserver:\n  shutdown_timeout: invalid",
		"version: 1\nadmin:\n  bind: 0.0.0.0:9090", "version: 1\nadmin:\n  auth_required: false",
		"version: 1\nsecurity:\n  allow_direct: true", "version: 1\ninspect:\n  enabled: true",
		"version: 1\nlisteners: []", "version: 1\ncache:\n  dns:\n    max_bytes: 0",
		"version: 1\nlisteners:\n  - name: http\n    type: http\n    bind: 127.0.0.1:8080\n    auth: local\n    policy: default\n    max_connections: 0",
		strings.Repeat(" ", MaxDocumentBytes+1),
	} {
		t.Run(doc[:min(len(doc), 60)], func(t *testing.T) {
			_, err := Load(Options{Home: t.TempDir(), File: strings.NewReader(doc)})
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("accepted %q: %v", doc[:min(len(doc), 100)], err)
			}
			if strings.Contains(err.Error(), "fake-secret") {
				t.Fatal("decoder echoed secret")
			}
		})
	}
}
func TestOverridesAndDurations(t *testing.T) {
	doc := "version: 1\nserver:\n  shutdown_timeout: 7s\nlisteners:\n  - name: only\n    type: http\n    bind: '[::1]:8888'\n    auth: local\n    policy: default\n"
	e, err := Load(Options{Home: t.TempDir(), File: strings.NewReader(doc)})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Config.Listeners) != 1 || e.Config.Listeners[0].MaxConnections != 256 || e.Config.Server.ShutdownTimeout != config.Duration(7_000_000_000) {
		t.Fatalf("%+v", e.Config)
	}
	for _, opts := range []Options{
		{Env: map[string]string{"PROXYSIEVE_UNKNOWN": "fake-secret"}},
		{Env: map[string]string{"PROXYSIEVE_ADMIN_BIND": "127.0.0.1:9090", "PROXYSIEVE_API_BIND": "127.0.0.1:9091"}},
		{Flags: map[string]string{"security.allow_direct": "not-bool"}},
		{Flags: map[string]string{"server.unknown": "whatever"}},
		{Flags: map[string]string{"traffic.retention_days": "3.5"}},
	} {
		opts.Home = t.TempDir()
		if _, err := Load(opts); err == nil {
			t.Fatal("invalid override accepted")
		}
	}
}

func TestDataDirectoryDerivation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chosen")
	e, err := Load(Options{Home: t.TempDir(), Env: map[string]string{"PROXYSIEVE_DATA_DIR": dir}})
	if err != nil || e.Config.Storage.Path != filepath.Join(dir, "proxysieve.db") || e.Sources["storage.path"] != "derived:server.data_dir" {
		t.Fatal(e, err)
	}
	path := filepath.Join(t.TempDir(), "explicit.db")
	e, err = Load(Options{Home: t.TempDir(), Env: map[string]string{"PROXYSIEVE_DATA_DIR": dir, "PROXYSIEVE_STORAGE_PATH": path}})
	if err != nil || e.Config.Storage.Path != path {
		t.Fatal(e, err)
	}
}

func TestNumericPrecedence(t *testing.T) {
	e, err := Load(Options{Home: t.TempDir(), Flags: map[string]string{"traffic.retention_days": "7"}, Runtime: map[string]string{"traffic.retention_days": "14"}})
	if err != nil || e.Config.Traffic.RetentionDays != 14 || e.Sources["traffic.retention_days"] != "runtime" {
		t.Fatal(e, err)
	}
}
func FuzzLoad(f *testing.F) {
	f.Add("version: 1\nlogging:\n  level: info\n")
	f.Add("version: 2")
	f.Add("---")
	f.Fuzz(func(t *testing.T, doc string) {
		if len(doc) > MaxDocumentBytes+1 {
			t.Skip()
		}
		e, err := Load(Options{Home: "local-test", File: strings.NewReader(doc)})
		if err == nil && e.Config.Validate() != nil {
			t.Fatal("returned invalid config")
		}
	})
}
