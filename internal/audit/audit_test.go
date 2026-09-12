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

func TestMemoryAuditCursorKeepsSameTimestampEvents(t *testing.T) {
	m, _ := NewMemory(10)
	at := time.Now().UTC()
	for _, event := range []Event{
		{ID: "first", At: at, Action: "proxy.created", TargetType: "proxy"},
		{ID: "second", At: at, Action: "proxy.created", TargetType: "proxy"},
		{ID: "third", At: at, Action: "proxy.created", TargetType: "proxy"},
	} {
		if err := m.Record(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	page, err := m.ListAudit(context.Background(), Page{Limit: 2})
	if err != nil || len(page) != 2 || page[0].ID != "third" || page[1].ID != "second" {
		t.Fatalf("first page=%v err=%v", page, err)
	}
	page, err = m.ListAudit(context.Background(), Page{Before: at, BeforeID: "second", Limit: 2})
	if err != nil || len(page) != 1 || page[0].ID != "first" {
		t.Fatalf("cursor page=%v err=%v", page, err)
	}
}
