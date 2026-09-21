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

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	pspolicy "github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	retrypkg "github.com/nguyenduytan/proxysieve/pkg/retry"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
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
	Version   int                   `json:"version" yaml:"version"`
	Server    Server                `json:"server" yaml:"server"`
	Listeners []Listener            `json:"listeners" yaml:"listeners"`
	Admin     Admin                 `json:"admin" yaml:"admin"`
	Storage   Storage               `json:"storage" yaml:"storage"`
	Traffic   Traffic               `json:"traffic" yaml:"traffic"`
	Health    Health                `json:"health" yaml:"health"`
	Retry     retrypkg.Policy       `json:"retry" yaml:"retry"`
	Inspect   Inspect               `json:"inspect" yaml:"inspect"`
	Cache     Cache                 `json:"cache" yaml:"cache"`
	Security  Security              `json:"security" yaml:"security"`
	Logging   Logging               `json:"logging" yaml:"logging"`
	Proxies   []proxy.Endpoint      `json:"proxies" yaml:"proxies"`
	Pools     []routing.Pool        `json:"pools" yaml:"pools"`
	Chains    []routing.Chain       `json:"chains" yaml:"chains"`
	Policies  []pspolicy.Policy     `json:"policies" yaml:"policies"`
	Budgets   []publicbudget.Config `json:"budgets" yaml:"budgets"`
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
	MinuteRetentionDays int      `json:"minute_retention_days" yaml:"minute_retention_days"`
	HourRetentionDays   int      `json:"hour_retention_days" yaml:"hour_retention_days"`
	DayRetentionDays    int      `json:"day_retention_days" yaml:"day_retention_days"`
	AggregationInterval Duration `json:"aggregation_interval" yaml:"aggregation_interval"`
	QueueCapacity       int      `json:"queue_capacity" yaml:"queue_capacity"`
	BatchSize           int      `json:"batch_size" yaml:"batch_size"`
	FlushInterval       Duration `json:"flush_interval" yaml:"flush_interval"`
}
type Health struct {
	FailureThreshold  uint32   `json:"failure_threshold" yaml:"failure_threshold"`
	SuccessThreshold  uint32   `json:"success_threshold" yaml:"success_threshold"`
	OpenDuration      Duration `json:"open_duration" yaml:"open_duration"`
	InitialScore      uint8    `json:"initial_score" yaml:"initial_score"`
	SuccessGain       uint8    `json:"success_gain" yaml:"success_gain"`
	FailurePenalty    uint8    `json:"failure_penalty" yaml:"failure_penalty"`
	Treat403AsFailure bool     `json:"treat_403_as_failure" yaml:"treat_403_as_failure"`
	Treat429AsFailure bool     `json:"treat_429_as_failure" yaml:"treat_429_as_failure"`
	Treat5xxAsFailure bool     `json:"treat_5xx_as_failure" yaml:"treat_5xx_as_failure"`
	ActiveChecks      bool     `json:"active_checks" yaml:"active_checks"`
	CheckHost         string   `json:"check_host" yaml:"check_host"`
	CheckPort         uint16   `json:"check_port" yaml:"check_port"`
	CheckInterval     Duration `json:"check_interval" yaml:"check_interval"`
	CheckTimeout      Duration `json:"check_timeout" yaml:"check_timeout"`
	GlobalCheckRate   uint32   `json:"global_checks_per_minute" yaml:"global_checks_per_minute"`
	PoolCheckRate     uint32   `json:"pool_checks_per_minute" yaml:"pool_checks_per_minute"`
}

func (h Health) RuntimeConfig() publichealth.Config {
	return publichealth.Config{
		FailureThreshold: h.FailureThreshold, SuccessThreshold: h.SuccessThreshold,
		OpenDuration: time.Duration(h.OpenDuration), InitialScore: h.InitialScore,
		SuccessGain: h.SuccessGain, FailurePenalty: h.FailurePenalty,
		Treat403AsFailure: h.Treat403AsFailure, Treat429AsFailure: h.Treat429AsFailure,
		Treat5xxAsFailure: h.Treat5xxAsFailure,
	}
}

type Inspect struct {
	Enabled   bool     `json:"enabled" yaml:"enabled"`
	Include   []string `json:"include" yaml:"include"`
	Exclude   []string `json:"exclude" yaml:"exclude"`
	OnFailure string   `json:"on_failure" yaml:"on_failure"`
}
type Cache struct {
	Response CacheLimit `json:"response" yaml:"response"`
	DNS      DNSCache   `json:"dns" yaml:"dns"`
}
type CacheLimit struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	MaxEntries int    `json:"max_entries" yaml:"max_entries"`
	MaxBytes   int64  `json:"max_bytes" yaml:"max_bytes"`
	Driver     string `json:"driver" yaml:"driver"`
	Path       string `json:"path" yaml:"path"`
}
type DNSCache struct {
	Enabled    bool     `json:"enabled" yaml:"enabled"`
	MaxEntries int      `json:"max_entries" yaml:"max_entries"`
	MaxBytes   int64    `json:"max_bytes" yaml:"max_bytes"`
	TTL        Duration `json:"ttl" yaml:"ttl"`
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
		Traffic:  Traffic{RetentionDays: 30, MinuteRetentionDays: 90, HourRetentionDays: 365, DayRetentionDays: 3650, AggregationInterval: Duration(time.Minute), QueueCapacity: 4096, BatchSize: 128, FlushInterval: Duration(250 * time.Millisecond)},
		Health:   Health{FailureThreshold: 3, SuccessThreshold: 2, OpenDuration: Duration(time.Minute), InitialScore: 50, SuccessGain: 5, FailurePenalty: 15, Treat429AsFailure: true, Treat5xxAsFailure: true, CheckHost: "example.com", CheckPort: 443, CheckInterval: Duration(5 * time.Minute), CheckTimeout: Duration(15 * time.Second), GlobalCheckRate: 60, PoolCheckRate: 30},
		Retry:    retrypkg.DefaultPolicy(),
		Inspect:  Inspect{OnFailure: "reject"},
		Cache:    Cache{DNS: DNSCache{Enabled: true, MaxEntries: 4096, MaxBytes: 4 << 20, TTL: Duration(time.Minute)}, Response: CacheLimit{MaxEntries: 1024, MaxBytes: 64 << 20, Driver: "memory", Path: filepath.Join(dir, "response-cache")}},
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
		if l.Type != "http" && l.Type != "socks5" || l.Auth != "local" && l.Auth != "password" && l.Auth != "api_key" || !model.ID(l.Policy).Valid() {
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
	if c.Traffic.RetentionDays < 1 || c.Traffic.RetentionDays > 3650 ||
		c.Traffic.MinuteRetentionDays < 1 || c.Traffic.MinuteRetentionDays > 3650 ||
		c.Traffic.HourRetentionDays < 1 || c.Traffic.HourRetentionDays > 3650 ||
		c.Traffic.DayRetentionDays < 1 || c.Traffic.DayRetentionDays > 3650 ||
		c.Traffic.AggregationInterval <= 0 || c.Traffic.AggregationInterval > Duration(time.Hour) ||
		c.Traffic.QueueCapacity < 1 || c.Traffic.QueueCapacity > 1_000_000 ||
		c.Traffic.BatchSize < 1 || c.Traffic.BatchSize > c.Traffic.QueueCapacity ||
		c.Traffic.FlushInterval < Duration(10*time.Millisecond) || c.Traffic.FlushInterval > Duration(time.Minute) {
		return ErrInvalid
	}
	if c.Health.RuntimeConfig().Validate() != nil || c.Health.CheckPort == 0 || !proxy.ValidHost(c.Health.CheckHost) || c.Health.CheckInterval < Duration(time.Minute) || c.Health.CheckInterval > Duration(24*time.Hour) || c.Health.CheckTimeout <= 0 || c.Health.CheckTimeout > Duration(time.Minute) || c.Health.GlobalCheckRate < 1 || c.Health.GlobalCheckRate > 60_000 || c.Health.PoolCheckRate < 1 || c.Health.PoolCheckRate > 60_000 {
		return ErrInvalid
	}
	if c.Retry.Validate() != nil || c.Retry.MaxAttempts == 0 {
		return ErrInvalid
	}
	for _, v := range []CacheLimit{{Enabled: c.Cache.DNS.Enabled, MaxEntries: c.Cache.DNS.MaxEntries, MaxBytes: c.Cache.DNS.MaxBytes}, c.Cache.Response} {
		if v.MaxEntries < 1 || v.MaxEntries > 1_000_000 || v.MaxBytes < 1 || v.MaxBytes > 1<<40 {
			return ErrInvalid
		}
	}
	if c.Cache.Response.Driver != "memory" && c.Cache.Response.Driver != "disk" || c.Cache.Response.Path == "" || strings.ContainsRune(c.Cache.Response.Path, 0) || c.Cache.Response.Driver == "disk" && !pathWithin(c.Server.DataDir, c.Cache.Response.Path) {
		return ErrInvalid
	}
	if c.Cache.DNS.TTL < Duration(time.Second) || c.Cache.DNS.TTL > Duration(24*time.Hour) {
		return ErrInvalid
	}
	if c.Inspect.Enabled && len(c.Inspect.Include) == 0 {
		return ErrInvalid
	}
	if c.Inspect.OnFailure != "reject" && c.Inspect.OnFailure != "tunnel" || !validInspectPatterns(c.Inspect.Include) || !validInspectPatterns(c.Inspect.Exclude) {
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
	chainIDs := map[model.ID]bool{}
	for _, chain := range c.Chains {
		if chain.Validate() != nil || chainIDs[chain.ID] {
			return ErrInvalid
		}
		chainEndpoints := map[model.ID]bool{}
		for _, hop := range chain.Hops {
			pool, exists := poolIDs[hop.PoolID]
			if !exists || pool.SessionPolicy.Normalized().Strategy != publicsession.None {
				return ErrInvalid
			}
			for endpointID := range poolEndpointIDs(hop.PoolID, poolIDs, map[model.ID]bool{}) {
				if chainEndpoints[endpointID] {
					return ErrInvalid
				}
				chainEndpoints[endpointID] = true
			}
		}
		chainIDs[chain.ID] = true
	}
	policyIDs := map[model.ID]bool{}
	for _, document := range c.Policies {
		if document.Validate() != nil || policyIDs[document.ID] {
			return ErrInvalid
		}
		policyIDs[document.ID] = true
		for _, rule := range document.Rules {
			for _, action := range rule.Actions {
				switch action.Type {
				case "proxy":
					if _, exists := poolIDs[action.PoolID]; !exists {
						return ErrInvalid
					}
				case "chain":
					if !chainIDs[action.ChainID] {
						return ErrInvalid
					}
					for _, fallback := range action.FallbackChainIDs {
						if !chainIDs[fallback] {
							return ErrInvalid
						}
					}
				}
			}
		}
	}
	budgetIDs := map[model.ID]bool{}
	for _, configured := range c.Budgets {
		if configured.Validate() != nil || budgetIDs[configured.ID] {
			return ErrInvalid
		}
		if configured.Scope == publicbudget.ScopePool {
			if _, ok := poolIDs[configured.ScopeID]; !ok {
				return ErrInvalid
			}
		}
		if configured.Scope == publicbudget.ScopeProxy && !proxyIDs[configured.ScopeID] {
			return ErrInvalid
		}
		budgetIDs[configured.ID] = true
	}
	for _, listener := range c.Listeners {
		if !policyIDs[model.ID(listener.Policy)] {
			return ErrInvalid
		}
	}
	return nil
}

func poolEndpointIDs(id model.ID, pools map[model.ID]routing.Pool, seen map[model.ID]bool) map[model.ID]bool {
	result := map[model.ID]bool{}
	if seen[id] {
		return result
	}
	seen[id] = true
	pool := pools[id]
	for _, endpointID := range pool.EndpointIDs {
		result[endpointID] = true
	}
	for _, fallbackID := range pool.FallbackPoolIDs {
		for endpointID := range poolEndpointIDs(fallbackID, pools, seen) {
			result[endpointID] = true
		}
	}
	return result
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

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validInspectPatterns(patterns []string) bool {
	seen := map[string]bool{}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSuffix(pattern, "."))
		base := strings.TrimPrefix(pattern, "*.")
		if len(pattern) > 253 || !proxy.ValidHost(base) || seen[pattern] {
			return false
		}
		seen[pattern] = true
	}
	return true
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
	c.Chains = slices.Clone(c.Chains)
	for i := range c.Chains {
		c.Chains[i] = c.Chains[i].Clone()
	}
	c.Policies = slices.Clone(c.Policies)
	for i := range c.Policies {
		c.Policies[i] = c.Policies[i].Clone()
	}
	c.Budgets = slices.Clone(c.Budgets)
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
