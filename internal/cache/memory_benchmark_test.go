package cache

import (
	"testing"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/cache"
)

var benchmarkCacheEntry Entry

func BenchmarkResponseCacheHit(b *testing.B) {
	cache, err := NewMemory(1, 2<<10)
	if err != nil {
		b.Fatal(err)
	}
	key := public.Key{ClientID: "client", SessionHash: "session", RouteID: "route", Method: "GET", URL: "https://example.invalid/asset"}
	now := time.Now().UTC()
	if !cache.Put(key, Entry{Status: 200, Header: map[string][]string{"Content-Type": {"application/octet-stream"}}, Body: make([]byte, 1<<10), ExpiresAt: now.Add(time.Hour)}) {
		b.Fatal("seed cache")
	}
	b.SetBytes(1 << 10)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		entry, ok := cache.Get(key, now)
		if !ok {
			b.Fatal("cache miss")
		}
		benchmarkCacheEntry = entry
	}
}
