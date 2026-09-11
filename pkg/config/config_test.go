package config

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSafeDefaultsAndManager(t *testing.T) {
	c := Defaults(t.TempDir())
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Inspect.Enabled || c.Security.AllowDirect || !c.Security.DenyPrivate || c.Cache.Response.Enabled || c.Logging.CaptureBodies {
		t.Fatal("unsafe defaults")
	}
	m, err := NewManager(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Listeners[0].Bind = "0.0.0.0:8080"
	if m.Snapshot().Config.Listeners[0].Bind != "127.0.0.1:8080" {
		t.Fatal("input aliased")
	}
	bad := m.Snapshot()
	bad.Config.Version = 2
	if _, err = m.Apply(1, bad.Config); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if m.Snapshot().Revision != 1 {
		t.Fatal("invalid config published")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			snap := m.Snapshot()
			snap.Config.Logging.Level = "debug"
			_, err := m.Apply(1, snap.Config)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 || m.Snapshot().Revision != 2 {
		t.Fatal("stale writers accepted")
	}
	snap := m.Snapshot()
	snap.Config.Listeners[0].Bind = "mutated"
	if m.Snapshot().Config.Listeners[0].Bind == "mutated" {
		t.Fatal("snapshot aliased")
	}
}
