package health

import (
	public "github.com/nguyenduytan/proxysieve/pkg/health"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time      { return c.now }
func (c *fakeClock) Add(d time.Duration) { c.now = c.now.Add(d) }
func TestCircuitTransitions(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	config := public.Defaults()
	config.FailureThreshold = 2
	config.SuccessThreshold = 2
	config.OpenDuration = time.Minute
	m, err := New(config, clock)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Observe("proxy", public.Observation{Success: false})
	if s.State != public.Degraded || s.Circuit != public.CircuitClosed {
		t.Fatal(s)
	}
	s, _ = m.Observe("proxy", public.Observation{Success: false})
	if s.State != public.Quarantined || s.Circuit != public.CircuitOpen || s.Score >= config.InitialScore {
		t.Fatal(s)
	}
	s, _ = m.Observe("proxy", public.Observation{Success: true})
	if s.Circuit != public.CircuitOpen {
		t.Fatal("open accepted ordinary traffic", s)
	}
	clock.Add(time.Minute)
	s, _ = m.Get("proxy", clock.Now())
	if s.Circuit != public.CircuitHalfOpen {
		t.Fatal(s)
	}
	s, _ = m.Observe("proxy", public.Observation{Success: true})
	if s.Circuit != public.CircuitHalfOpen {
		t.Fatal(s)
	}
	s, _ = m.Observe("proxy", public.Observation{Success: true})
	if s.Circuit != public.CircuitClosed || s.State != public.Healthy {
		t.Fatal(s)
	}
}
func TestStatusWeightingAndDisable(t *testing.T) {
	config := public.Defaults()
	config.FailureThreshold = 1
	config.Treat403AsFailure = false
	m, _ := New(config, nil)
	s, _ := m.Observe("proxy", public.Observation{Success: true, HTTPStatus: 403})
	if s.Circuit != public.CircuitClosed {
		t.Fatal(s)
	}
	s, _ = m.Observe("proxy", public.Observation{Success: true, HTTPStatus: 429})
	if s.Circuit != public.CircuitOpen {
		t.Fatal(s)
	}
	s, _ = m.SetDisabled("other", true)
	if s.State != public.Disabled {
		s, _ = m.SetDisabled("other", true)
		t.Fatal(s)
	}
	s, _ = m.SetDisabled("other", false)
	if s.State != public.Unknown {
		t.Fatal(s)
	}
}
func TestEligibility(t *testing.T) {
	clock := &fakeClock{now: time.Now().UTC()}
	config := public.Defaults()
	config.FailureThreshold = 1
	config.OpenDuration = time.Minute
	m, _ := New(config, clock)
	if !m.Eligible("new", clock.Now()) {
		t.Fatal("unknown should be eligible")
	}
	_, _ = m.Observe("proxy", public.Observation{Success: false})
	if m.Eligible("proxy", clock.Now()) {
		t.Fatal("open circuit eligible")
	}
	clock.Add(time.Minute)
	if !m.Eligible("proxy", clock.Now()) {
		t.Fatal("half-open probe unavailable")
	}
}
