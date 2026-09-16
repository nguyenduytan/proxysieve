package cache

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	public "github.com/nguyenduytan/proxysieve/pkg/cache"
)

type dnsEntry struct {
	addresses []netip.Addr
	expiresAt time.Time
	bytes     int64
}

// DNS is a bounded positive-result cache around a resolver.
type DNS struct {
	mu         sync.Mutex
	base       security.Resolver
	entries    map[string]dnsEntry
	maxEntries int
	maxBytes   int64
	ttl        time.Duration
	bytes      int64
	now        func() time.Time
}

func NewDNS(base security.Resolver, maxEntries int, maxBytes int64, ttl time.Duration) (*DNS, error) {
	if base == nil || maxEntries < 1 || maxBytes < 1 || ttl <= 0 {
		return nil, public.ErrInvalid
	}
	return &DNS{base: base, entries: map[string]dnsEntry{}, maxEntries: maxEntries, maxBytes: maxBytes, ttl: ttl, now: time.Now}, nil
}

func (d *DNS) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	key := strings.ToLower(strings.TrimSuffix(host, "."))
	now := d.now()
	d.mu.Lock()
	entry, ok := d.entries[key]
	if ok && !entry.expiresAt.After(now) {
		d.bytes -= entry.bytes
		delete(d.entries, key)
		ok = false
	}
	d.mu.Unlock()
	if ok {
		return append([]netip.Addr(nil), entry.addresses...), nil
	}

	// ponytail: concurrent misses may duplicate DNS work; add singleflight only if resolver load becomes measurable.
	addresses, err := d.base.LookupNetIP(ctx, host)
	if err != nil || len(addresses) == 0 || key == "" {
		return addresses, err
	}
	entry = dnsEntry{addresses: append([]netip.Addr(nil), addresses...), expiresAt: d.now().Add(d.ttl)}
	entry.bytes = int64(len(key))
	for _, address := range entry.addresses {
		entry.bytes += int64(len(address.String()))
	}
	if entry.bytes > d.maxBytes {
		return addresses, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.removeExpired(d.now())
	old, exists := d.entries[key]
	for (!exists && len(d.entries) >= d.maxEntries) || d.bytes-old.bytes+entry.bytes > d.maxBytes {
		if !d.evict(key) {
			return addresses, nil
		}
	}
	d.entries[key] = entry
	d.bytes += entry.bytes - old.bytes
	return addresses, nil
}

func (d *DNS) removeExpired(now time.Time) {
	for key, entry := range d.entries {
		if !entry.expiresAt.After(now) {
			d.bytes -= entry.bytes
			delete(d.entries, key)
		}
	}
}

func (d *DNS) evict(except string) bool {
	var candidate string
	var expires time.Time
	for key, entry := range d.entries {
		if key == except || candidate != "" && (entry.expiresAt.After(expires) || entry.expiresAt.Equal(expires) && key > candidate) {
			continue
		}
		candidate, expires = key, entry.expiresAt
	}
	if candidate == "" {
		return false
	}
	d.bytes -= d.entries[candidate].bytes
	delete(d.entries, candidate)
	return true
}
