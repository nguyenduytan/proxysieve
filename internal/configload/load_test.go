package configload

import (
	"encoding/json"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
func TestMissingHealthSectionUsesDefaults(t *testing.T) {
	home := t.TempDir()
	effective, err := Load(Options{Home: home, File: strings.NewReader("version: 1\n")})
	if err != nil || effective.Config.Health != config.Defaults(home).Health || effective.Sources["health.check_host"] != "default" {
		t.Fatal(effective.Config.Health, err)
	}
}

func TestMissingRetrySectionUsesDefaults(t *testing.T) {
	home := t.TempDir()
	effective, err := Load(Options{Home: home, File: strings.NewReader("version: 1\n")})
	if err != nil || effective.Config.Retry != config.Defaults(home).Retry || effective.Sources["retry.max_attempts"] != "default" {
		t.Fatal(effective.Config.Retry, err)
	}
	effective, err = Load(Options{Home: home, File: strings.NewReader("version: 1\nretry:\n  max_attempts: 1\n  allow_idempotency_key: true\n")})
	if err != nil || effective.Config.Retry.MaxAttempts != 1 || !effective.Config.Retry.AllowIdempotencyKey || effective.Sources["retry.max_attempts"] != "file" {
		t.Fatal(effective.Config.Retry, err)
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

func TestChainHopDurationLoadsFromYAMLAndRemainsNumericJSON(t *testing.T) {
	doc := `version: 1
proxies:
  - id: first-proxy
    name: First proxy
    protocol: http
    host: first.example.com
    port: 8080
    enabled: true
    trusted_remote_dns: true
  - id: second-proxy
    name: Second proxy
    protocol: http
    host: second.example.com
    port: 8080
    enabled: true
    trusted_remote_dns: true
pools:
  - id: first-pool
    name: First pool
    strategy: round-robin
    endpoint_ids: [first-proxy]
    fallback_pool_ids: []
    required_tags: []
    country: ""
    min_health_score: 0
    max_latency: 0s
    enabled: true
  - id: second-pool
    name: Second pool
    strategy: round-robin
    endpoint_ids: [second-proxy]
    fallback_pool_ids: []
    required_tags: []
    country: ""
    min_health_score: 0
    max_latency: 0s
    enabled: true
chains:
  - id: ordered-chain
    name: Ordered chain
    enabled: true
    hops:
      - pool: first-pool
        timeout: 10s
      - pool: second-pool
        timeout: 20s
`
	effective, err := Load(Options{Home: t.TempDir(), File: strings.NewReader(doc)})
	if err != nil || len(effective.Config.Chains) != 1 || effective.Config.Chains[0].Hops[0].EffectiveTimeout() != 10*time.Second {
		t.Fatal(effective.Config.Chains, err)
	}
	raw, err := json.Marshal(effective.Config.Chains[0])
	if err != nil || !strings.Contains(string(raw), `"timeout_ns":10000000000`) {
		t.Fatal(string(raw), err)
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
	e, err := Load(Options{Home: t.TempDir(), Flags: map[string]string{"traffic.retention_days": "7", "traffic.minute_retention_days": "45"}, Runtime: map[string]string{"traffic.retention_days": "14"}})
	if err != nil || e.Config.Traffic.RetentionDays != 14 || e.Config.Traffic.MinuteRetentionDays != 45 || e.Sources["traffic.retention_days"] != "runtime" || e.Sources["traffic.minute_retention_days"] != "flags" {
		t.Fatal(e, err)
	}
}

func TestSecretEnvironmentIsNotConfig(t *testing.T) {
	e, err := Load(Options{Home: t.TempDir(), Env: map[string]string{"PROXYSIEVE_SECRET_UPSTREAM_AUTH": `{"username":"demo","password":"fake-password"}`}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := e.Sources["PROXYSIEVE_SECRET_UPSTREAM_AUTH"]; exists {
		t.Fatal("secret reported as config")
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
