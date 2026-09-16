// Package retry defines conservative retry eligibility separate from transports.
package retry

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"
)

var ErrInvalid = errors.New("invalid retry policy")

type Failure string

const (
	ConnectFailure  Failure = "connect"
	TimeoutFailure  Failure = "timeout"
	ResetFailure    Failure = "reset"
	ResponseFailure Failure = "response"
)

type Policy struct {
	MaxAttempts         uint8 `json:"max_attempts" yaml:"max_attempts"`
	AllowIdempotencyKey bool  `json:"allow_idempotency_key" yaml:"allow_idempotency_key"`
}

func DefaultPolicy() Policy { return Policy{MaxAttempts: 2, AllowIdempotencyKey: false} }
func (p Policy) Validate() error {
	if p.MaxAttempts > 5 {
		return ErrInvalid
	}
	return nil
}

type Request struct {
	Method            string
	BodyPresent       bool
	BodyReplayable    bool
	ResponseDelivered bool
	IdempotencyKey    bool
	Attempt           uint8
	Failure           Failure
}

func (p Policy) ShouldRetry(request Request) bool {
	if p.Validate() != nil || p.MaxAttempts == 0 || request.Attempt >= p.MaxAttempts || request.ResponseDelivered {
		return false
	}
	if request.Failure != ConnectFailure && request.Failure != TimeoutFailure && request.Failure != ResetFailure {
		return false
	}
	if !safeMethod(request.Method) {
		if !p.AllowIdempotencyKey || !request.IdempotencyKey {
			return false
		}
	}
	return !request.BodyPresent || request.BodyReplayable
}

func Wait(ctx context.Context, attempt uint8) error {
	if attempt == 0 {
		attempt = 1
	}
	delay := 25 * time.Millisecond << min(attempt-1, 4)
	var random [1]byte
	_, _ = rand.Read(random[:])
	timer := time.NewTimer(delay + time.Duration(random[0])*delay/512)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodPut || method == http.MethodDelete
}
