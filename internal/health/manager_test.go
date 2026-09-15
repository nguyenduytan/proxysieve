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
	if m.Eligible("other", time.Now().UTC()) || m.Acquire("other", time.Now().UTC()) {
		t.Fatal("disabled endpoint became eligible")
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
	if !m.Acquire("proxy", clock.Now()) || m.Eligible("proxy", clock.Now()) || m.Acquire("proxy", clock.Now()) {
		t.Fatal("more than one half-open probe was admitted")
	}
	_, _ = m.Observe("proxy", public.Observation{Success: true})
	if !m.Acquire("proxy", clock.Now()) {
		t.Fatal("completed half-open probe was not released")
	}
}

func TestLatencyUsesRollingAverage(t *testing.T) {
	m, _ := New(public.Defaults(), nil)
	_, _ = m.Observe("proxy", public.Observation{Success: true, Latency: 100 * time.Millisecond, ConnectLatency: 80 * time.Millisecond, TTFB: 120 * time.Millisecond})
	state, _ := m.Observe("proxy", public.Observation{Success: true, Latency: 20 * time.Millisecond, ConnectLatency: 40 * time.Millisecond, TTFB: 40 * time.Millisecond})
	if state.Latency != 80*time.Millisecond || state.ConnectLatency != 70*time.Millisecond || state.TTFB != 100*time.Millisecond {
		t.Fatal(state)
	}
}

func TestRollingMetricsClassifyAndBound(t *testing.T) {
	config := public.Defaults()
	config.FailureThreshold = 1000
	m, _ := New(config, nil)
	for _, observation := range []public.Observation{
		{Success: true, HTTPStatus: 403},
		{Success: true, HTTPStatus: 407},
		{Success: true, HTTPStatus: 429},
		{Success: true, HTTPStatus: 500},
		{Success: false, Timeout: true, DNSFailure: true},
		{Success: false, TLSFailure: true},
		{Success: true, AuthFailure: true},
	} {
		_, _ = m.Observe("proxy", observation)
	}
	state, _ := m.Get("proxy", time.Now().UTC())
	if state.Observations != 7 || state.Successes != 1 || state.Failures != 6 || state.Timeouts != 1 || state.AuthFailures != 2 || state.DNSFailures != 1 || state.TLSFailures != 1 || state.Status403 != 1 || state.Status407 != 1 || state.Status429 != 1 || state.Status5xx != 1 {
		t.Fatal(state)
	}
	for range metricWindowSize {
		_, _ = m.Observe("proxy", public.Observation{Success: true})
	}
	state, _ = m.Get("proxy", time.Now().UTC())
	if state.Observations != metricWindowSize || state.Successes != metricWindowSize || state.Failures != 0 || state.Timeouts != 0 || state.AuthFailures != 0 || state.DNSFailures != 0 || state.TLSFailures != 0 || state.Status403 != 0 || state.Status407 != 0 || state.Status429 != 0 || state.Status5xx != 0 {
		t.Fatal(state)
	}
}

func TestThroughputUsesRollingAverage(t *testing.T) {
	m, _ := New(public.Defaults(), nil)
	_, _ = m.Observe("proxy", public.Observation{Success: true})
	if err := m.RecordThroughput("proxy", 1_000, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordThroughput("proxy", 2_000, time.Second); err != nil {
		t.Fatal(err)
	}
	state, _ := m.Get("proxy", time.Now().UTC())
	if state.ThroughputBytesPerSec != 1_250 {
		t.Fatal(state.ThroughputBytesPerSec)
	}
	if err := m.RecordThroughput("proxy", ^uint64(0), time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	state, _ = m.Get("proxy", time.Now().UTC())
	if state.ThroughputBytesPerSec < 1_250 {
		t.Fatal("throughput overflowed", state.ThroughputBytesPerSec)
	}
}
