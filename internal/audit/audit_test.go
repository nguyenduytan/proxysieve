package audit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryAudit(t *testing.T) {
	m, _ := NewMemory(2)
	now := time.Now().UTC()
	for _, event := range []Event{{ID: "one", At: now.Add(-time.Minute), ActorID: "actor", Action: "proxy.created", TargetType: "proxy", TargetID: "one"}, {ID: "two", At: now, ActorID: "actor", Action: "proxy.created", TargetType: "proxy", TargetID: "two"}, {ID: "three", At: now.Add(time.Minute), ActorID: "actor", Action: "proxy.created", TargetType: "proxy", TargetID: "three"}} {
		if err := m.Record(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := m.ListAudit(context.Background(), Page{Limit: 2})
	if err != nil || len(events) != 2 || events[0].ID != "three" || events[1].ID != "two" {
		t.Fatal(events, err)
	}
}
