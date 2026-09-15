// Package health implements an in-memory, caller-driven health state machine.
package health

import (
	"errors"
	"sync"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var ErrNotFound = errors.New("health state not found")

type Clock interface{ Now() time.Time }
type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

type Manager struct {
	mu     sync.Mutex
	config public.Config
	clock  Clock
	states map[model.ID]public.Snapshot
	probes map[model.ID]bool
	recent map[model.ID][]sample
}

type sample struct {
	failed, timeout, auth, dns, tls, status403, status407, status429, status5xx bool
}

const metricWindowSize = 100

func New(config public.Config, source Clock) (*Manager, error) {
	if config.Validate() != nil {
		return nil, public.ErrInvalid
	}
	if source == nil {
		source = clock{}
	}
	return &Manager{config: config, clock: source, states: map[model.ID]public.Snapshot{}, probes: map[model.ID]bool{}, recent: map[model.ID][]sample{}}, nil
}
func (m *Manager) Observe(id model.ID, observation public.Observation) (public.Snapshot, error) {
	if !id.Valid() {
		return public.Snapshot{}, public.ErrInvalid
	}
	if observation.At.IsZero() {
		observation.At = m.clock.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.probes, id)
	state, ok := m.states[id]
	if !ok {
		state = public.Snapshot{EndpointID: id, State: public.Unknown, Circuit: public.CircuitClosed, Score: m.config.InitialScore}
	}
	if state.State == public.Disabled {
		m.states[id] = state
		return state, nil
	}
	if state.Circuit == public.CircuitOpen && observation.At.Before(state.OpenUntil) {
		m.states[id] = state
		return state, nil
	}
	if state.Circuit == public.CircuitOpen {
		state.Circuit = public.CircuitHalfOpen
		state.State = public.HalfOpen
		state.ConsecutiveSuccesses = 0
	}
	if observation.Latency > 0 {
		state.Latency = rollingDuration(state.Latency, observation.Latency)
	}
	if observation.ConnectLatency > 0 {
		state.ConnectLatency = rollingDuration(state.ConnectLatency, observation.ConnectLatency)
	}
	if observation.TTFB > 0 {
		state.TTFB = rollingDuration(state.TTFB, observation.TTFB)
	}
	failed := failure(m.config, observation)
	state = m.track(id, state, observation, failed)
	if failed {
		state.ConsecutiveFailures++
		state.ConsecutiveSuccesses = 0
		state.LastFailure = observation.At
		state.Score = decrease(state.Score, m.config.FailurePenalty)
		if state.ConsecutiveFailures >= m.config.FailureThreshold || state.Circuit == public.CircuitHalfOpen {
			state.State = public.Quarantined
			state.Circuit = public.CircuitOpen
			state.OpenUntil = observation.At.Add(m.config.OpenDuration)
		} else {
			state.State = public.Degraded
		}
	} else {
		state.ConsecutiveSuccesses++
		state.ConsecutiveFailures = 0
		state.LastSuccess = observation.At
		state.Score = increase(state.Score, m.config.SuccessGain)
		if state.Circuit == public.CircuitHalfOpen && state.ConsecutiveSuccesses >= m.config.SuccessThreshold {
			state.Circuit = public.CircuitClosed
			state.State = public.Healthy
			state.OpenUntil = time.Time{}
		} else if state.State == public.Unknown || state.State == public.Degraded {
			state.State = public.Healthy
		}
	}
	m.states[id] = state
	return state, nil
}

func (m *Manager) track(id model.ID, state public.Snapshot, observation public.Observation, failed bool) public.Snapshot {
	entry := sample{
		failed: failed, timeout: observation.Timeout,
		auth: observation.AuthFailure || observation.HTTPStatus == 407,
		dns:  observation.DNSFailure, tls: observation.TLSFailure,
		status403: observation.HTTPStatus == 403, status407: observation.HTTPStatus == 407,
		status429: observation.HTTPStatus == 429,
		status5xx: observation.HTTPStatus >= 500 && observation.HTTPStatus <= 599,
	}
	recent := m.recent[id]
	if len(recent) == metricWindowSize {
		copy(recent, recent[1:])
		recent[len(recent)-1] = entry
	} else {
		recent = append(recent, entry)
	}
	m.recent[id] = recent
	state.Observations, state.Successes, state.Failures = uint32(len(recent)), 0, 0
	state.Timeouts, state.AuthFailures, state.DNSFailures, state.TLSFailures = 0, 0, 0, 0
	state.Status403, state.Status407, state.Status429, state.Status5xx = 0, 0, 0, 0
	for _, item := range recent {
		if item.failed {
			state.Failures++
		} else {
			state.Successes++
		}
		if item.timeout {
			state.Timeouts++
		}
		if item.auth {
			state.AuthFailures++
		}
		if item.dns {
			state.DNSFailures++
		}
		if item.tls {
			state.TLSFailures++
		}
		if item.status403 {
			state.Status403++
		}
		if item.status407 {
			state.Status407++
		}
		if item.status429 {
			state.Status429++
		}
		if item.status5xx {
			state.Status5xx++
		}
	}
	return state
}

func (m *Manager) RecordThroughput(id model.ID, bytes uint64, elapsed time.Duration) error {
	if !id.Valid() || bytes == 0 || elapsed <= 0 {
		return public.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[id]
	if !ok {
		return ErrNotFound
	}
	rateValue := float64(bytes) / elapsed.Seconds()
	rate := ^uint64(0)
	if rateValue < float64(rate) {
		rate = uint64(rateValue)
	}
	if state.ThroughputBytesPerSec == 0 {
		state.ThroughputBytesPerSec = rate
	} else {
		state.ThroughputBytesPerSec = uint64((float64(state.ThroughputBytesPerSec)*3 + float64(rate)) / 4)
	}
	m.states[id] = state
	return nil
}

func rollingDuration(current, next time.Duration) time.Duration {
	if current == 0 {
		return next
	}
	return current + (next-current)/4
}
func (m *Manager) Get(id model.ID, now time.Time) (public.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[id]
	if !ok {
		return public.Snapshot{}, ErrNotFound
	}
	state = afterCooldown(state, now)
	m.states[id] = state
	return state, nil
}

// Eligible returns true for unseen endpoints; unknown health is selectable until
// actual observations say otherwise. Open circuits transition to half-open after
// their cooldown and allow the next controlled attempt.
func (m *Manager) Eligible(id model.ID, now time.Time) bool {
	_, eligible := m.EligibleSnapshot(id, now)
	return eligible
}

func (m *Manager) EligibleSnapshot(id model.ID, now time.Time) (public.Snapshot, bool) {
	if !id.Valid() {
		return public.Snapshot{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[id]
	if !ok {
		state = public.Snapshot{EndpointID: id, State: public.Unknown, Circuit: public.CircuitClosed, Score: m.config.InitialScore}
	}
	state = afterCooldown(state, now)
	if ok {
		m.states[id] = state
	}
	return state, state.Eligible(now) && !m.probes[id]
}

// Acquire allows one in-flight attempt while an endpoint is half-open.
func (m *Manager) Acquire(id model.ID, now time.Time) bool {
	if !id.Valid() {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[id]
	if !ok {
		return true
	}
	state = afterCooldown(state, now)
	m.states[id] = state
	if !state.Eligible(now) || m.probes[id] {
		return false
	}
	if state.Circuit == public.CircuitHalfOpen {
		m.probes[id] = true
	}
	return true
}

func (m *Manager) Release(id model.ID) {
	m.mu.Lock()
	delete(m.probes, id)
	m.mu.Unlock()
}
func (m *Manager) SetDisabled(id model.ID, disabled bool) (public.Snapshot, error) {
	if !id.Valid() {
		return public.Snapshot{}, public.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.probes, id)
	state, ok := m.states[id]
	if !ok {
		state = public.Snapshot{EndpointID: id, State: public.Unknown, Circuit: public.CircuitClosed, Score: m.config.InitialScore}
	}
	if disabled {
		state.State = public.Disabled
		state.Circuit = public.CircuitOpen
	} else if state.State == public.Disabled {
		state.State = public.Unknown
		state.Circuit = public.CircuitClosed
		state.OpenUntil = time.Time{}
	}
	m.states[id] = state
	return state, nil
}

func afterCooldown(state public.Snapshot, now time.Time) public.Snapshot {
	if state.State != public.Disabled && state.Circuit == public.CircuitOpen && !now.Before(state.OpenUntil) {
		state.Circuit = public.CircuitHalfOpen
		state.State = public.HalfOpen
		state.ConsecutiveSuccesses = 0
	}
	return state
}
func failure(config public.Config, observation public.Observation) bool {
	if !observation.Success || observation.AuthFailure || observation.Timeout {
		return true
	}
	if observation.HTTPStatus == 403 && config.Treat403AsFailure {
		return true
	}
	if observation.HTTPStatus == 407 {
		return true
	}
	if observation.HTTPStatus == 429 && config.Treat429AsFailure {
		return true
	}
	return observation.HTTPStatus >= 500 && observation.HTTPStatus <= 599 && config.Treat5xxAsFailure
}
func increase(score, gain uint8) uint8 {
	if 100-score < gain {
		return 100
	}
	return score + gain
}
func decrease(score, penalty uint8) uint8 {
	if score < penalty {
		return 0
	}
	return score - penalty
}
