package events

import (
	"context"
	"fmt"
	"testing"
	"time"

	publicevent "github.com/nguyenduytan/proxysieve/pkg/event"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

func TestBusBoundsHistoryAndStreams(t *testing.T) {
	bus, err := New(2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stream := bus.Subscribe(ctx)
	for _, id := range []string{"one", "two", "three"} {
		if err = bus.Publish(t.Context(), publicevent.Event{ID: model.ID(id), At: time.Now(), Type: "test.event", Severity: publicevent.Info, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	items, dropped, err := bus.Snapshot(10)
	if err != nil || dropped != 1 || len(items) != 2 || items[0].ID != "three" || items[1].ID != "two" {
		t.Fatal(items, dropped, err)
	}
	if event := <-stream; event.ID != "one" {
		t.Fatal(event)
	}
	cancel()
}

func TestBusDisconnectsSlowSubscriber(t *testing.T) {
	bus, err := New(100)
	if err != nil {
		t.Fatal(err)
	}
	stream := bus.Subscribe(t.Context())
	for index := range 65 {
		if err = bus.Publish(t.Context(), publicevent.Event{ID: model.ID(fmt.Sprintf("event-%d", index)), At: time.Now(), Type: "test.event", Severity: publicevent.Info, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	for range 64 {
		<-stream
	}
	if _, open := <-stream; open {
		t.Fatal("slow subscriber remained connected")
	}
}
