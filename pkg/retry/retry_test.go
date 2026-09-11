package retry

import (
	"net/http"
	"testing"
)

func TestConservativeEligibility(t *testing.T) {
	p := DefaultPolicy()
	for _, request := range []Request{{Method: http.MethodGet, Attempt: 0, Failure: ConnectFailure}, {Method: http.MethodPut, BodyReplayable: true, Attempt: 1, Failure: TimeoutFailure}, {Method: http.MethodDelete, BodyReplayable: true, Attempt: 1, Failure: ResetFailure}} {
		if !p.ShouldRetry(request) {
			t.Fatal(request)
		}
	}
	for _, request := range []Request{{Method: http.MethodPost, BodyReplayable: true, Attempt: 0, Failure: ConnectFailure}, {Method: http.MethodPut, BodyReplayable: false, Attempt: 0, Failure: ConnectFailure}, {Method: http.MethodGet, ResponseDelivered: true, Attempt: 0, Failure: ConnectFailure}, {Method: http.MethodGet, Attempt: 2, Failure: ConnectFailure}, {Method: http.MethodGet, Attempt: 0, Failure: ResponseFailure}} {
		if p.ShouldRetry(request) {
			t.Fatal(request)
		}
	}
	p.AllowIdempotencyKey = true
	if !p.ShouldRetry(Request{Method: http.MethodPost, BodyReplayable: true, IdempotencyKey: true, Attempt: 0, Failure: ConnectFailure}) {
		t.Fatal("idempotent post")
	}
}
