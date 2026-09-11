// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package config defines versioned configuration without parser/storage dependencies.
package config

import (
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	pspolicy "github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

var ErrInvalid = errors.New("invalid configuration")
var ErrConflict = errors.New("configuration revision conflict")

type Duration time.Duration

func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return ErrInvalid
	}
	*d = Duration(v)
	return nil
}
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }
func (d *Duration) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) != nil {
		return ErrInvalid
	}
	return d.UnmarshalText([]byte(value))
}

type Config struct {
	Version   int               `json:"version" yaml:"version"`
	Server    Server            `json:"server" yaml:"server"`
	Listeners []Listener        `json:"listeners" yaml:"listeners"`
	Admin     Admin             `json:"admin" yaml:"admin"`
	Storage   Storage           `json:"storage" yaml:"storage"`
	Traffic   Traffic           `json:"traffic" yaml:"traffic"`
	Inspect   Inspect           `json:"inspect" yaml:"inspect"`
	Cache     Cache             `json:"cache" yaml:"cache"`
	Security  Security          `json:"security" yaml:"security"`
	Logging   Logging           `json:"logging" yaml:"logging"`
	Proxies   []proxy.Endpoint  `json:"proxies" yaml:"proxies"`
	Pools     []routing.Pool    `json:"pools" yaml:"pools"`
	Policies  []pspolicy.Policy `json:"policies" yaml:"policies"`
}
type Server struct {
	DataDir         string   `json:"data_dir" yaml:"data_dir"`
	ShutdownTimeout Duration `json:"shutdown_timeout" yaml:"shutdown_timeout"`
}
type Listener struct {
	Name           string     `json:"name" yaml:"name"`
	Type           string     `json:"type" yaml:"type"`
	Bind           string     `json:"bind" yaml:"bind"`
	Auth           string     `json:"auth" yaml:"auth"`
	CredentialRef  secret.Ref `json:"credential_ref,omitempty" yaml:"credential_ref,omitempty"`
	Policy         string     `json:"policy" yaml:"policy"`
	MaxConnections int        `json:"max_connections" yaml:"max_connections"`
	IdleTimeout    Duration   `json:"idle_timeout" yaml:"idle_timeout"`
}
type Admin struct {
	Enabled      bool       `json:"enabled" yaml:"enabled"`
	Bind         string     `json:"bind" yaml:"bind"`
	TLS          bool       `json:"tls" yaml:"tls"`
	CertFile     string     `json:"cert_file,omitempty" yaml:"cert_file,omitempty"`
	KeyRef       secret.Ref `json:"key_ref,omitempty" yaml:"key_ref,omitempty"`
	AuthRequired bool       `json:"auth_required" yaml:"auth_required"`
}
type Storage struct {
	Driver      string   `json:"driver" yaml:"driver"`
	Path        string   `json:"path" yaml:"path"`
	BusyTimeout Duration `json:"busy_timeout" yaml:"busy_timeout"`
}
type Traffic struct {
	RetentionDays       int      `json:"retention_days" yaml:"retention_days"`
	AggregationInterval Duration `json:"aggregation_interval" yaml:"aggregation_interval"`
}
type Inspect struct {
	Enabled bool     `json:"enabled" yaml:"enabled"`
	Include []string `json:"include" yaml:"include"`
	Exclude []string `json:"exclude" yaml:"exclude"`
}
type Cache struct {
	Response CacheLimit `json:"response" yaml:"response"`
	DNS      CacheLimit `json:"dns" yaml:"dns"`
}
type CacheLimit struct {
	Enabled    bool  `json:"enabled" yaml:"enabled"`
	MaxEntries int   `json:"max_entries" yaml:"max_entries"`
	MaxBytes   int64 `json:"max_bytes" yaml:"max_bytes"`
}
type Security struct {
	DenyPrivate     bool     `json:"deny_private_networks_for_untrusted_clients" yaml:"deny_private_networks_for_untrusted_clients"`
	AllowDirect     bool     `json:"allow_direct" yaml:"allow_direct"`
	DirectAllowlist []string `json:"direct_allowlist" yaml:"direct_allowlist"`
}
type Logging struct {
	Level          string `json:"level" yaml:"level"`
	Format         string `json:"format" yaml:"format"`
	CaptureHeaders bool   `json:"capture_headers" yaml:"capture_headers"`
	CaptureBodies  bool   `json:"capture_bodies" yaml:"capture_bodies"`
}

func Defaults(home string) Config {
	dir := filepath.Join(home, ".proxysieve")
	return Config{
		Version: 1,
		Server:  Server{DataDir: dir, ShutdownTimeout: Duration(20 * time.Second)},
		Listeners: []Listener{
			{Name: "http", Type: "http", Bind: "127.0.0.1:8080", Auth: "local", Policy: "default", MaxConnections: 256, IdleTimeout: Duration(2 * time.Minute)},
			{Name: "socks", Type: "socks5", Bind: "127.0.0.1:1080", Auth: "local", Policy: "default", MaxConnections: 256, IdleTimeout: Duration(2 * time.Minute)},
		},
		Admin:    Admin{Enabled: true, Bind: "127.0.0.1:9090", AuthRequired: true},
		Storage:  Storage{Driver: "sqlite", Path: filepath.Join(dir, "proxysieve.db"), BusyTimeout: Duration(5 * time.Second)},
		Traffic:  Traffic{RetentionDays: 30, AggregationInterval: Duration(time.Minute)},
		Cache:    Cache{DNS: CacheLimit{Enabled: true, MaxEntries: 4096, MaxBytes: 4 << 20}, Response: CacheLimit{MaxEntries: 1024, MaxBytes: 64 << 20}},
		Security: Security{DenyPrivate: true},
		Logging:  Logging{Level: "info", Format: "json"},
		Policies: []pspolicy.Policy{{Version: 1, ID: "default", Name: "Fail closed", Rules: []pspolicy.Rule{{ID: "deny", Name: "Deny requests until configured", Priority: 100, Enabled: true, StopProcessing: true, Actions: []pspolicy.Action{{Type: "reject"}}}}}},
	}
}

// Validate rejects unsafe unauthenticated non-loopback bindings and unbounded use.
// It checks syntax only: it does not open listeners, read keys, or perform DNS.
func (c Config) Validate() error {
	if c.Version != 1 || c.Server.DataDir == "" || strings.ContainsRune(c.Server.DataDir, 0) || c.Server.ShutdownTimeout <= 0 || c.Server.ShutdownTimeout > Duration(5*time.Minute) {
		return ErrInvalid
	}
	if len(c.Listeners) == 0 || len(c.Listeners) > 32 {
		return ErrInvalid
	}
	names := map[string]bool{}
	bindings := map[netip.AddrPort]bool{}
	for _, l := range c.Listeners {
		ap, err := netip.ParseAddrPort(l.Bind)
		if err != nil || ap.Port() == 0 || ap.Addr().Zone() != "" || bindings[ap] || !model.ID(l.Name).Valid() || names[l.Name] {
			return ErrInvalid
		}
		if l.Type != "http" && l.Type != "socks5" || l.Auth != "local" && l.Auth != "password" || !model.ID(l.Policy).Valid() {
			return ErrInvalid
		}
		if !ap.Addr().IsLoopback() && l.Auth == "local" || l.Auth == "password" && !l.CredentialRef.Valid() {
			return ErrInvalid
		}
		if l.CredentialRef != "" && !l.CredentialRef.Valid() {
			return ErrInvalid
		}
		if l.MaxConnections < 1 || l.MaxConnections > 100_000 || l.IdleTimeout <= 0 || l.IdleTimeout > Duration(24*time.Hour) {
			return ErrInvalid
		}
		names[l.Name] = true
		bindings[ap] = true
	}
	if c.Admin.KeyRef != "" && !c.Admin.KeyRef.Valid() {
		return ErrInvalid
	}
	if c.Admin.Enabled {
		ap, err := netip.ParseAddrPort(c.Admin.Bind)
		if err != nil || ap.Port() == 0 || ap.Addr().Zone() != "" || bindings[ap] || !c.Admin.AuthRequired {
			return ErrInvalid
		}
		if !ap.Addr().IsLoopback() && !c.Admin.TLS {
			return ErrInvalid
		}
		if c.Admin.TLS && (c.Admin.CertFile == "" || !c.Admin.KeyRef.Valid()) {
			return ErrInvalid
		}
	}
	if c.Storage.Driver != "sqlite" && c.Storage.Driver != "memory" || c.Storage.BusyTimeout <= 0 || c.Storage.BusyTimeout > Duration(time.Minute) {
		return ErrInvalid
	}
	if c.Storage.Driver == "sqlite" && (c.Storage.Path == "" || strings.HasPrefix(c.Storage.Path, "file:") || strings.ContainsRune(c.Storage.Path, 0)) {
		return ErrInvalid
	}
	if c.Traffic.RetentionDays < 1 || c.Traffic.RetentionDays > 3650 || c.Traffic.AggregationInterval <= 0 || c.Traffic.AggregationInterval > Duration(time.Hour) {
		return ErrInvalid
	}
	for _, v := range []CacheLimit{c.Cache.DNS, c.Cache.Response} {
		if v.MaxEntries < 1 || v.MaxEntries > 1_000_000 || v.MaxBytes < 1 || v.MaxBytes > 1<<40 {
			return ErrInvalid
		}
	}
	if c.Inspect.Enabled && len(c.Inspect.Include) == 0 {
		return ErrInvalid
	}
	if c.Security.AllowDirect && len(c.Security.DirectAllowlist) == 0 {
		return ErrInvalid
	}
	if len(c.Security.DirectAllowlist) > 4096 || len(c.Inspect.Include) > 4096 || len(c.Inspect.Exclude) > 4096 {
		return ErrInvalid
	}
	switch c.Logging.Level {
	case "trace", "debug", "info", "warn", "error":
	default:
		return ErrInvalid
	}
	if c.Logging.Format != "json" && c.Logging.Format != "console" {
		return ErrInvalid
	}
	proxyIDs := map[model.ID]bool{}
	for _, endpoint := range c.Proxies {
		if endpoint.Validate() != nil || proxyIDs[endpoint.ID] {
			return ErrInvalid
		}
		proxyIDs[endpoint.ID] = true
	}
	poolIDs := map[model.ID]routing.Pool{}
	for _, pool := range c.Pools {
		if pool.Validate() != nil {
			return ErrInvalid
		}
		if _, exists := poolIDs[pool.ID]; exists {
			return ErrInvalid
		}
		for _, id := range pool.EndpointIDs {
			if !proxyIDs[id] {
				return ErrInvalid
			}
		}
		poolIDs[pool.ID] = pool
	}
	for _, pool := range c.Pools {
		for _, fallback := range pool.FallbackPoolIDs {
			if _, exists := poolIDs[fallback]; !exists {
				return ErrInvalid
			}
		}
	}
	if hasPoolCycle(poolIDs) {
		return ErrInvalid
	}
	policyIDs := map[model.ID]bool{}
	for _, document := range c.Policies {
		if document.Validate() != nil || policyIDs[document.ID] {
			return ErrInvalid
		}
		policyIDs[document.ID] = true
		for _, rule := range document.Rules {
			for _, action := range rule.Actions {
				if action.Type == "proxy" {
					if _, exists := poolIDs[action.PoolID]; !exists {
						return ErrInvalid
					}
				}
			}
		}
	}
	for _, listener := range c.Listeners {
		if !policyIDs[model.ID(listener.Policy)] {
			return ErrInvalid
		}
	}
	return nil
}

func hasPoolCycle(pools map[model.ID]routing.Pool) bool {
	visiting := map[model.ID]bool{}
	done := map[model.ID]bool{}
	var visit func(model.ID) bool
	visit = func(id model.ID) bool {
		if visiting[id] {
			return true
		}
		if done[id] {
			return false
		}
		visiting[id] = true
		for _, next := range pools[id].FallbackPoolIDs {
			if visit(next) {
				return true
			}
		}
		visiting[id] = false
		done[id] = true
		return false
	}
	for id := range pools {
		if visit(id) {
			return true
		}
	}
	return false
}

func (c Config) Clone() Config {
	c.Listeners = slices.Clone(c.Listeners)
	c.Security.DirectAllowlist = slices.Clone(c.Security.DirectAllowlist)
	c.Inspect.Include = slices.Clone(c.Inspect.Include)
	c.Inspect.Exclude = slices.Clone(c.Inspect.Exclude)
	c.Proxies = slices.Clone(c.Proxies)
	for i := range c.Proxies {
		c.Proxies[i] = c.Proxies[i].Clone()
	}
	c.Pools = slices.Clone(c.Pools)
	for i := range c.Pools {
		c.Pools[i] = c.Pools[i].Clone()
	}
	c.Policies = slices.Clone(c.Policies)
	for i := range c.Policies {
		c.Policies[i] = c.Policies[i].Clone()
	}
	return c
}

type Snapshot struct {
	Revision uint64 `json:"revision"`
	Config   Config `json:"config"`
}

// Manager publishes validated immutable snapshots and rejects stale writers.
// It does not hot-apply structural listener changes; the runtime must classify
// restart-required fields before using it as a live reload boundary in M3/M5.
type Manager struct {
	mu      sync.RWMutex
	current Snapshot
}

func NewManager(c Config) (*Manager, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Manager{current: Snapshot{Revision: 1, Config: c.Clone()}}, nil
}
func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{Revision: m.current.Revision, Config: m.current.Config.Clone()}
}
func (m *Manager) Apply(expected uint64, c Config) (Snapshot, error) {
	if err := c.Validate(); err != nil {
		return Snapshot{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if expected != m.current.Revision || expected == ^uint64(0) {
		return Snapshot{}, ErrConflict
	}
	m.current = Snapshot{Revision: expected + 1, Config: c.Clone()}
	return Snapshot{Revision: m.current.Revision, Config: m.current.Config.Clone()}, nil
}
