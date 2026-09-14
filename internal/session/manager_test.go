package session

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	public "github.com/nguyenduytan/proxysieve/pkg/session"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"math"
	"sync"
	"testing"
	"time"
)

type fakeClock struct{ value time.Time }

func (c *fakeClock) Now() time.Time      { return c.value }
func (c *fakeClock) Add(d time.Duration) { c.value = c.value.Add(d) }
func TestStickyLifecycle(t *testing.T) {
	clock := &fakeClock{value: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m, err := New([]byte("a session test key that is long enough"), 10, clock)
	if err != nil {
		t.Fatal(err)
	}
	var selected int
	request := Request{ClientID: "client", PoolID: "pool", Key: "private-session-key", Policy: public.Policy{Strategy: "explicit", TTL: time.Hour, IdleTTL: time.Minute}, Select: func(context.Context) (model.ID, error) { selected++; return "proxy", nil }}
	first, err := m.Resolve(context.Background(), request)
	if err != nil || first.Reused || first.Session.KeyHash == "" || first.Session.KeyHash == request.Key {
		t.Fatal(first, err)
	}
	second, err := m.Resolve(context.Background(), request)
	if err != nil || !second.Reused || second.Session.ID != first.Session.ID || selected != 1 {
		t.Fatal(second, err, selected)
	}
	if err = m.RecordUsage(context.Background(), first.Session.ID, 5, 7); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Hour)
	third, err := m.Resolve(context.Background(), request)
	if err != nil || third.Reused || third.Session.ID == first.Session.ID || selected != 2 {
		t.Fatal(third, err, selected)
	}
	if err = m.Rotate(context.Background(), third.Session.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	fourth, err := m.Resolve(context.Background(), request)
	if err != nil || fourth.Reused || selected != 3 {
		t.Fatal(fourth, err, selected)
	}
}
func TestNoneAndConcurrentResolve(t *testing.T) {
	m, _ := New([]byte("a session test key that is long enough"), 2, nil)
	request := Request{ClientID: "client", PoolID: "pool", Policy: public.Policy{Strategy: "none"}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }}
	a, _ := m.Resolve(context.Background(), request)
	b, _ := m.Resolve(context.Background(), request)
	if a.Session.ID == b.Session.ID {
		t.Fatal("none persisted")
	}
	sticky := request
	sticky.Key = "key"
	sticky.Policy.Strategy = "explicit"
	var wg sync.WaitGroup
	var ids sync.Map
	for range 20 {
		wg.Go(func() {
			result, err := m.Resolve(context.Background(), sticky)
			if err != nil {
				t.Error(err)
			}
			ids.Store(result.Session.ID, true)
		})
	}
	wg.Wait()
	count := 0
	ids.Range(func(_, _ any) bool { count++; return true })
	if count != 1 {
		t.Fatal(count)
	}
	if err := m.RecordUsage(context.Background(), "missing", traffic.Bytes(1), 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSessionStrategiesAndRotationReasons(t *testing.T) {
	clock := &fakeClock{value: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m, err := New([]byte("a session test key that is long enough"), 20, clock)
	if err != nil {
		t.Fatal(err)
	}
	selected := 0
	selectEndpoint := func(context.Context) (model.ID, error) {
		selected++
		return model.ID("proxy-" + string(rune('a'+selected%2))), nil
	}
	for _, tc := range []struct {
		strategy public.Strategy
		key      string
	}{
		{public.Explicit, "explicit"},
		{public.Client, "client"},
		{public.Destination, "destination.example"},
		{public.ClientDestination, "client|destination.example"},
	} {
		request := Request{ClientID: "client", PoolID: "pool", Key: tc.key, RuntimeRevision: 1, Policy: public.Policy{Strategy: tc.strategy, TTL: time.Hour}, Select: selectEndpoint}
		first, resolveErr := m.Resolve(context.Background(), request)
		if resolveErr != nil || first.Reused {
			t.Fatalf("%s first resolve: %+v %v", tc.strategy, first, resolveErr)
		}
		second, resolveErr := m.Resolve(context.Background(), request)
		if resolveErr != nil || !second.Reused || second.Session.ID != first.Session.ID {
			t.Fatalf("%s second resolve: %+v %v", tc.strategy, second, resolveErr)
		}
		if rotateErr := m.Rotate(context.Background(), first.Session.ID, public.Manual); rotateErr != nil {
			t.Fatal(rotateErr)
		}
		third, resolveErr := m.Resolve(context.Background(), request)
		if resolveErr != nil || third.Reused || third.Session.RotationReason != public.Created {
			t.Fatalf("%s rotated resolve: %+v %v", tc.strategy, third, resolveErr)
		}
	}

	request := Request{ClientID: "client", PoolID: "limits", Key: "limits", RuntimeRevision: 1, Policy: public.Policy{Strategy: public.Explicit, TTL: time.Hour, IdleTTL: time.Minute, MaxRequests: 1, MaxBytes: 10}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }}
	first, err := m.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RecordUsage(context.Background(), first.Session.ID, 5, 5); err != nil {
		t.Fatal(err)
	}
	second, err := m.Resolve(context.Background(), request)
	if err != nil || second.Reused || second.Session.ID == first.Session.ID {
		t.Fatalf("limit rotation: %+v %v", second, err)
	}
	clock.Add(2 * time.Minute)
	third, err := m.Resolve(context.Background(), Request{ClientID: "client", PoolID: "idle", Key: "idle", RuntimeRevision: 1, Policy: public.Policy{Strategy: public.Explicit, IdleTTL: time.Minute}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Resolve(context.Background(), Request{ClientID: "client", PoolID: "idle", Key: "idle", RuntimeRevision: 1, Policy: public.Policy{Strategy: public.Explicit, IdleTTL: time.Minute}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }}); err != nil {
		t.Fatal(err)
	}
	if third.Session.ID == "" {
		t.Fatal("missing idle session ID")
	}
}

func TestSessionRevisionAndPolicyChangesRotate(t *testing.T) {
	m, _ := New([]byte("a session test key that is long enough"), 10, nil)
	selectEndpoint := func(context.Context) (model.ID, error) { return "proxy", nil }
	base := Request{ClientID: "client", PoolID: "pool", Key: "key", RuntimeRevision: 1, Policy: public.Policy{Strategy: public.Explicit, TTL: time.Hour}, Select: selectEndpoint}
	first, err := m.Resolve(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.RuntimeRevision = 2
	second, err := m.Resolve(context.Background(), changed)
	if err != nil || second.Reused || second.Session.ID == first.Session.ID {
		t.Fatalf("revision change did not rotate: %+v %v", second, err)
	}
	changed = base
	changed.Policy.MaxRequests = 2
	third, err := m.Resolve(context.Background(), changed)
	if err != nil || third.Reused || third.Session.ID == second.Session.ID {
		t.Fatalf("policy change did not rotate: %+v %v", third, err)
	}
}

func TestRecordUsageIsAtomicOnOverflow(t *testing.T) {
	m, err := New([]byte("a session test key that is long enough"), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{ClientID: "client", PoolID: "pool", Key: "key", Policy: public.Policy{Strategy: public.Explicit}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }}
	result, err := m.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	entry := m.sessions[string(result.Session.ID)]
	entry.UploadBytes = traffic.Bytes(math.MaxUint64 - 1)
	entry.RequestCount = math.MaxUint64
	m.sessions[string(result.Session.ID)] = entry
	m.mu.Unlock()
	if err = m.RecordUsage(context.Background(), result.Session.ID, 1, 0); !errors.Is(err, traffic.ErrOverflow) {
		t.Fatalf("want overflow, got %v", err)
	}
	m.mu.Lock()
	entry = m.sessions[string(result.Session.ID)]
	m.mu.Unlock()
	if entry.UploadBytes != traffic.Bytes(math.MaxUint64-1) || entry.RequestCount != math.MaxUint64 {
		t.Fatalf("overflow partially committed: %+v", entry)
	}
}
