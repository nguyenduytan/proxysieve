package session

import (
	"strings"
	"testing"
	"time"
)

func TestSessionValidation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := Session{ID: "session", ClientID: "client", KeyHash: strings.Repeat("a", 64), PoolID: "pool", ProxyEndpointID: "proxy", CreatedAt: now, LastUsedAt: now, Status: "active", RotationReason: Created, Policy: Policy{Strategy: Explicit}, RuntimeRevision: 1}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Session){
		func(value *Session) { value.KeyHash = "raw-key" },
		func(value *Session) { value.Status = "unknown" },
		func(value *Session) { value.RotationReason = Untracked },
		func(value *Session) { value.LastUsedAt = now.Add(-time.Second) },
		func(value *Session) { value.ExpiresAt = now.Add(-time.Second) },
	} {
		candidate := valid
		mutate(&candidate)
		if candidate.Validate() == nil {
			t.Fatalf("invalid session accepted: %+v", candidate)
		}
	}
}
