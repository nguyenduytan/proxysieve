package event

import (
	"testing"
	"time"
)

func TestEventValidation(t *testing.T) {
	valid := Event{ID: "event", At: time.Now(), Type: "proxy.created", Severity: Info, Source: "admin", ActorID: "user", TargetType: "proxy", TargetID: "proxy"}
	if !valid.Validate() {
		t.Fatal("valid event rejected")
	}
	for _, invalid := range []Event{
		{},
		{ID: "event", At: time.Now(), Type: "bad type", Severity: Info, Source: "admin"},
		{ID: "event", At: time.Now(), Type: "proxy.created", Severity: "fatal", Source: "admin"},
	} {
		if invalid.Validate() {
			t.Fatalf("invalid event accepted: %+v", invalid)
		}
	}
}
