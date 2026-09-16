package traffic

import (
	"context"
	"errors"
	"testing"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

func TestBoundedRecorder(t *testing.T) {
	m, err := NewMemory(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Record(context.Background(), public.Event{}); err != nil {
		t.Fatal(err)
	}
	if err = m.Record(context.Background(), public.Event{Action: "newest"}); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	events, dropped := m.Snapshot()
	if len(events) != 1 || events[0].Action != "newest" || dropped != 1 {
		t.Fatal(events, dropped)
	}
}

func TestSubscribersNeverBlockRecorder(t *testing.T) {
	m, _ := NewMemory(1)
	ctx, cancel := context.WithCancel(t.Context())
	stream := m.Subscribe(ctx)
	event := public.Event{Action: "proxy"}
	if err := m.Record(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-stream:
		if got.Action != event.Action {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive traffic")
	}
	for range 65 {
		_ = m.Record(t.Context(), event)
	}
	cancel()
	select {
	case _, open := <-stream:
		if open {
			for range stream {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber was not closed")
	}
}
