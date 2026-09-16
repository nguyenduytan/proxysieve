package cache

import (
	"testing"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/cache"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

func TestPartitionedMemoryCache(t *testing.T) {
	m, _ := NewMemory(2, 32)
	a := public.Key{ClientID: "clienta", SessionHash: "hasha", RouteID: "route", Method: "GET", URL: "https://example.invalid"}
	b := a
	b.ClientID = "clientb"
	entry := Entry{Status: 200, Body: []byte("safe"), ExpiresAt: time.Now().Add(time.Minute)}
	if !m.Put(a, entry) {
		t.Fatal("put")
	}
	if _, ok := m.Get(b, time.Now()); ok {
		t.Fatal("cross client leak")
	}
	got, ok := m.Get(a, time.Now())
	if !ok || string(got.Body) != "safe" {
		t.Fatal(got, ok)
	}
	got.Body[0] = 'x'
	again, _ := m.Get(a, time.Now())
	if string(again.Body) != "safe" {
		t.Fatal("alias")
	}
}

func TestMemoryCacheBoundsStatsAndPurge(t *testing.T) {
	m, _ := NewMemory(2, 8)
	now := time.Now().UTC()
	key := func(client string) public.Key {
		return public.Key{ClientID: model.ID(client), SessionHash: "hash", RouteID: "route", Method: "GET", URL: "https://example.invalid/" + client}
	}
	a, b, c := key("clienta"), key("clientb"), key("clientc")
	if !m.Put(a, Entry{Body: []byte("old"), ExpiresAt: now.Add(time.Minute)}) ||
		!m.Put(b, Entry{Body: []byte("bbbb"), ExpiresAt: now.Add(2 * time.Minute)}) {
		t.Fatal("seed cache")
	}
	if m.Put(a, Entry{Body: make([]byte, 9), ExpiresAt: now.Add(time.Minute)}) {
		t.Fatal("oversized replacement accepted")
	}
	if got, ok := m.Get(a, now); !ok || string(got.Body) != "old" {
		t.Fatal("failed replacement changed old entry")
	}
	if !m.Put(c, Entry{Body: []byte("cccc"), ExpiresAt: now.Add(3 * time.Minute)}) {
		t.Fatal("bounded eviction put")
	}
	if _, ok := m.Get(a, now); ok {
		t.Fatal("earliest expiry was not evicted")
	}
	if _, ok := m.Get(c, now); !ok {
		t.Fatal("new entry missing")
	}
	m.RecordBypass()
	stats := m.Stats(now)
	if stats.Entries != 2 || stats.Bytes != 8 || stats.MaxEntries != 2 || stats.MaxBytes != 8 || stats.Hits != 2 || stats.Misses != 1 || stats.Bypasses != 1 || stats.Evictions != 1 || stats.BytesServed != 7 || stats.HitRatio != 2.0/3.0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	removed := m.Purge()
	if removed.Entries != 2 || removed.Bytes != 8 || m.Stats(now).Entries != 0 {
		t.Fatalf("unexpected purge: %+v", removed)
	}
	if !m.Put(a, Entry{Body: []byte("old"), ExpiresAt: now.Add(-time.Second)}) {
		t.Fatal("put expired entry")
	}
	stats = m.Stats(now)
	if stats.Entries != 0 || stats.Bytes != 0 || stats.Expired != 1 {
		t.Fatalf("expired entry retained: %+v", stats)
	}
}
