package cache

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

type countingResolver struct {
	addresses []netip.Addr
	err       error
	calls     int
}

func (r *countingResolver) LookupNetIP(context.Context, string) ([]netip.Addr, error) {
	r.calls++
	return append([]netip.Addr(nil), r.addresses...), r.err
}

func TestDNSCachesPositiveResultsWithinBounds(t *testing.T) {
	base := &countingResolver{addresses: []netip.Addr{netip.MustParseAddr("203.0.113.1")}}
	cache, err := NewDNS(base, 1, 64, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0)
	cache.now = func() time.Time { return now }

	first, err := cache.LookupNetIP(context.Background(), "Example.COM.")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = netip.MustParseAddr("192.0.2.1")
	second, err := cache.LookupNetIP(context.Background(), "example.com")
	if err != nil || base.calls != 1 || second[0] != netip.MustParseAddr("203.0.113.1") {
		t.Fatal(second, base.calls, err)
	}

	if _, err = cache.LookupNetIP(context.Background(), "other.example"); err != nil || base.calls != 2 {
		t.Fatal(base.calls, err)
	}
	if _, err = cache.LookupNetIP(context.Background(), "example.com"); err != nil || base.calls != 3 {
		t.Fatal("entry bound did not evict earliest entry", base.calls, err)
	}

	now = now.Add(time.Minute)
	if _, err = cache.LookupNetIP(context.Background(), "example.com"); err != nil || base.calls != 4 {
		t.Fatal("expired result was reused", base.calls, err)
	}
}

func TestDNSDoesNotCacheFailuresOrEmptyResults(t *testing.T) {
	base := &countingResolver{err: errors.New("lookup failed")}
	cache, _ := NewDNS(base, 2, 64, time.Minute)
	for range 2 {
		if _, err := cache.LookupNetIP(context.Background(), "example.com"); err == nil {
			t.Fatal("lookup failure hidden")
		}
	}
	base.err = nil
	for range 2 {
		if addresses, err := cache.LookupNetIP(context.Background(), "empty.example"); err != nil || len(addresses) != 0 {
			t.Fatal(addresses, err)
		}
	}
	if base.calls != 4 {
		t.Fatal("failure or empty result was cached", base.calls)
	}

	base.addresses = []netip.Addr{netip.MustParseAddr("2001:db8::1")}
	tiny, _ := NewDNS(base, 2, 1, time.Minute)
	for range 2 {
		if _, err := tiny.LookupNetIP(context.Background(), "large.example"); err != nil {
			t.Fatal(err)
		}
	}
	if base.calls != 6 {
		t.Fatal("result larger than byte bound was cached", base.calls)
	}
}
