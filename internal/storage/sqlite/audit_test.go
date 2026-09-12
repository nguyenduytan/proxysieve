package sqlite

import (
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"testing"
	"time"
)

func TestAuditPersistence(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	first := time.Now().UTC().Add(-time.Minute)
	if err = s.Record(t.Context(), audit.Event{ID: "one", At: first, ActorID: "actor", Action: "proxy.created", TargetType: "proxy", TargetID: "one"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(t.Context(), audit.Event{ID: "two", At: time.Now().UTC(), ActorID: "actor", Action: "proxy.created", TargetType: "proxy", TargetID: "two"}); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListAudit(t.Context(), audit.Page{Limit: 10})
	if err != nil || len(events) != 2 || events[0].ID != "two" {
		t.Fatal(events, err)
	}
	events, err = s.ListAudit(t.Context(), audit.Page{Before: events[0].At, Limit: 10})
	if err != nil || len(events) != 1 || events[0].ID != "one" {
		t.Fatal(events, err)
	}
}

func TestAuditCursorKeepsSameTimestampEvents(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	at := time.Now().UTC()
	for _, id := range []string{"first", "second", "third"} {
		if err = s.Record(t.Context(), audit.Event{ID: model.ID(id), At: at, Action: "proxy.created", TargetType: "proxy"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListAudit(t.Context(), audit.Page{Limit: 2})
	if err != nil || len(page) != 2 || page[0].ID != "third" || page[1].ID != "second" {
		t.Fatalf("first page=%v err=%v", page, err)
	}
	page, err = s.ListAudit(t.Context(), audit.Page{Before: at, BeforeID: "second", Limit: 2})
	if err != nil || len(page) != 1 || page[0].ID != "first" {
		t.Fatalf("cursor page=%v err=%v", page, err)
	}
}
