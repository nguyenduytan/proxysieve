// Package cache implements a bounded, partitioned in-memory response metadata cache.
package cache

import (
	public "github.com/nguyenduytan/proxysieve/pkg/cache"
	"sync"
	"time"
)

type Entry struct {
	Status    int
	Header    map[string][]string
	Body      []byte
	ExpiresAt time.Time
}
type Memory struct {
	mu         sync.Mutex
	entries    map[string]Entry
	maxEntries int
	maxBytes   int64
	bytes      int64
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
		}
		return Entry{}, false
	}
	return clone(entry), true
}
func (m *Memory) Put(key public.Key, entry Entry) bool {
	if key.Validate() != nil || entry.ExpiresAt.IsZero() || int64(len(entry.Body)) > m.maxBytes {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := key.String()
	_, exists := m.entries[id]
	if old, ok := m.entries[id]; ok {
		m.bytes -= int64(len(old.Body))
	}
	if (!exists && len(m.entries) >= m.maxEntries) || m.bytes+int64(len(entry.Body)) > m.maxBytes {
		return false
	}
	m.entries[id] = clone(entry)
	m.bytes += int64(len(entry.Body))
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
