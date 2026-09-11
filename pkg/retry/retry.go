// Package retry defines conservative retry eligibility separate from transports.
package retry

import (
	"errors"
	"net/http"
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
	MaxAttempts         uint8
	AllowIdempotencyKey bool
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
	return !hasBody(request.Method) || request.BodyReplayable
}
func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodPut || method == http.MethodDelete
}
func hasBody(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
