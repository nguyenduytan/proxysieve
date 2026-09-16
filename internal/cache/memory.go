// Package cache implements a bounded, partitioned in-memory response metadata cache.
package cache

import (
	"net/url"
	"strings"
	"sync"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/cache"
)

type Entry struct {
	Status    int
	Header    map[string][]string
	Body      []byte
	ExpiresAt time.Time
	host      string
}
type Memory struct {
	mu         sync.Mutex
	entries    map[string]Entry
	maxEntries int
	maxBytes   int64
	bytes      int64
	hits       uint64
	misses     uint64
	bypasses   uint64
	expired    uint64
	evictions  uint64
	served     uint64
}

type Stats struct {
	Entries     int     `json:"entries"`
	Bytes       int64   `json:"bytes_stored"`
	MaxEntries  int     `json:"max_entries"`
	MaxBytes    int64   `json:"max_bytes"`
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	Bypasses    uint64  `json:"bypasses"`
	Expired     uint64  `json:"expired"`
	Evictions   uint64  `json:"evictions"`
	BytesServed uint64  `json:"bytes_served"`
	HitRatio    float64 `json:"hit_ratio"`
}

type PurgeResult struct {
	Entries int   `json:"entries"`
	Bytes   int64 `json:"bytes"`
}

func NewMemory(maxEntries int, maxBytes int64) (*Memory, error) {
	if maxEntries < 1 || maxBytes < 1 {
		return nil, public.ErrInvalid
	}
	return &Memory{entries: map[string]Entry{}, maxEntries: maxEntries, maxBytes: maxBytes}, nil
}
func (m *Memory) Get(key public.Key, now time.Time) (Entry, bool) {
	if key.Validate() != nil {
		return Entry{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key.String()]
	if !ok || !entry.ExpiresAt.After(now) {
		if ok {
			m.bytes -= int64(len(entry.Body))
			delete(m.entries, key.String())
			m.expired++
		}
		m.misses++
		return Entry{}, false
	}
	m.hits++
	m.served += uint64(len(entry.Body))
	return clone(entry), true
}

func (m *Memory) RecordBypass() {
	m.mu.Lock()
	m.bypasses++
	m.mu.Unlock()
}
func (m *Memory) Put(key public.Key, entry Entry) bool {
	if key.Validate() != nil || entry.ExpiresAt.IsZero() || int64(len(entry.Body)) > m.maxBytes {
		return false
	}
	parsed, err := url.Parse(key.URL)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	entry.host = strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	m.mu.Lock()
	defer m.mu.Unlock()
	id := key.String()
	old, exists := m.entries[id]
	oldBytes := int64(len(old.Body))
	m.removeExpired(time.Now().UTC(), id)
	for (!exists && len(m.entries) >= m.maxEntries) || m.bytes-oldBytes+int64(len(entry.Body)) > m.maxBytes {
		if !m.evict(id) {
			return false
		}
	}
	m.entries[id] = clone(entry)
	m.bytes += int64(len(entry.Body)) - oldBytes
	return true
}

func (m *Memory) Stats(now time.Time) Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeExpired(now, "")
	ratio := float64(0)
	if requests := m.hits + m.misses; requests > 0 {
		ratio = float64(m.hits) / float64(requests)
	}
	return Stats{Entries: len(m.entries), Bytes: m.bytes, MaxEntries: m.maxEntries, MaxBytes: m.maxBytes, Hits: m.hits, Misses: m.misses, Bypasses: m.bypasses, Expired: m.expired, Evictions: m.evictions, BytesServed: m.served, HitRatio: ratio}
}

func (m *Memory) Purge() PurgeResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := PurgeResult{Entries: len(m.entries), Bytes: m.bytes}
	m.entries = map[string]Entry{}
	m.bytes = 0
	return result
}

func (m *Memory) PurgeDomain(domain string) PurgeResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	result := PurgeResult{}
	for id, entry := range m.entries {
		if entry.host != domain {
			continue
		}
		result.Entries++
		result.Bytes += int64(len(entry.Body))
		delete(m.entries, id)
	}
	m.bytes -= result.Bytes
	return result
}

func (m *Memory) removeExpired(now time.Time, except string) {
	for id, entry := range m.entries {
		if id != except && !entry.ExpiresAt.After(now) {
			m.bytes -= int64(len(entry.Body))
			delete(m.entries, id)
			m.expired++
		}
	}
}

func (m *Memory) evict(except string) bool {
	var candidate string
	var expires time.Time
	for id, entry := range m.entries {
		if id == except || candidate != "" && (entry.ExpiresAt.After(expires) || entry.ExpiresAt.Equal(expires) && id > candidate) {
			continue
		}
		candidate, expires = id, entry.ExpiresAt
	}
	if candidate == "" {
		return false
	}
	m.bytes -= int64(len(m.entries[candidate].Body))
	delete(m.entries, candidate)
	m.evictions++
	return true
}
func clone(entry Entry) Entry {
	headers := make(map[string][]string, len(entry.Header))
	for name, values := range entry.Header {
		headers[name] = append([]string(nil), values...)
	}
	entry.Header = headers
	entry.Body = append([]byte(nil), entry.Body...)
	return entry
}
