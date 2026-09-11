// Package health implements an in-memory, caller-driven health state machine.
package health

import (
	"errors"
	public "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"sync"
	"time"
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
}

func New(config public.Config, source Clock) (*Manager, error) {
	if config.Validate() != nil {
		return nil, public.ErrInvalid
	}
	if source == nil {
		source = clock{}
	}
	return &Manager{config: config, clock: source, states: map[model.ID]public.Snapshot{}}, nil
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
	failed := failure(m.config, observation)
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
func (m *Manager) Get(id model.ID, now time.Time) (public.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[id]
	if !ok {
		return public.Snapshot{}, ErrNotFound
	}
	if state.Circuit == public.CircuitOpen && !now.Before(state.OpenUntil) {
		state.Circuit = public.CircuitHalfOpen
		state.State = public.HalfOpen
		m.states[id] = state
	}
	return state, nil
}

// Eligible returns true for unseen endpoints; unknown health is selectable until
// actual observations say otherwise. Open circuits transition to half-open after
// their cooldown and allow the next controlled attempt.
func (m *Manager) Eligible(id model.ID, now time.Time) bool {
	state, err := m.Get(id, now)
	if errors.Is(err, ErrNotFound) {
		return true
	}
	return err == nil && state.State != public.Disabled && state.Circuit != public.CircuitOpen
}
func (m *Manager) SetDisabled(id model.ID, disabled bool) (public.Snapshot, error) {
	if !id.Valid() {
		return public.Snapshot{}, public.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
func failure(config public.Config, observation public.Observation) bool {
	if !observation.Success || observation.AuthFailure || observation.Timeout {
		return true
	}
	if observation.HTTPStatus == 403 && config.Treat403AsFailure {
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
