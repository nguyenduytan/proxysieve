// Package health defines explainable proxy health and circuit breaker state.
package health

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"time"
)

var ErrInvalid = errors.New("invalid health configuration")

type State string

const (
	Unknown     State = "unknown"
	Healthy     State = "healthy"
	Degraded    State = "degraded"
	Quarantined State = "quarantined"
	HalfOpen    State = "half_open"
	Disabled    State = "disabled"
)

type Circuit string

const (
	CircuitClosed   Circuit = "closed"
	CircuitOpen     Circuit = "open"
	CircuitHalfOpen Circuit = "half_open"
)

type Config struct {
	FailureThreshold  uint32
	SuccessThreshold  uint32
	OpenDuration      time.Duration
	InitialScore      uint8
	SuccessGain       uint8
	FailurePenalty    uint8
	Treat403AsFailure bool
	Treat429AsFailure bool
	Treat5xxAsFailure bool
}

func (c Config) Validate() error {
	if c.FailureThreshold < 1 || c.SuccessThreshold < 1 || c.OpenDuration <= 0 || c.OpenDuration > 24*time.Hour {
		return ErrInvalid
	}
	return nil
}
func Defaults() Config {
	return Config{FailureThreshold: 3, SuccessThreshold: 2, OpenDuration: time.Minute, InitialScore: 50, SuccessGain: 5, FailurePenalty: 15, Treat429AsFailure: true, Treat5xxAsFailure: true}
}

type Observation struct {
	At          time.Time
	Success     bool
	HTTPStatus  int
	AuthFailure bool
	Timeout     bool
	Latency     time.Duration
	HealthCheck bool
}
type Snapshot struct {
	EndpointID           model.ID
	State                State
	Circuit              Circuit
	Score                uint8
	ConsecutiveFailures  uint32
	ConsecutiveSuccesses uint32
	LastSuccess          time.Time
	LastFailure          time.Time
	OpenUntil            time.Time
	ActiveConnections    uint64
}

func (s Snapshot) Eligible(now time.Time) bool {
	return s.State != Disabled && (s.Circuit == CircuitClosed || s.Circuit == CircuitHalfOpen && !now.Before(s.OpenUntil))
}
