package session

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	public "github.com/nguyenduytan/proxysieve/pkg/session"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
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
