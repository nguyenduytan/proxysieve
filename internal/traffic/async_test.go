package traffic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type batchStore struct {
	mu      sync.Mutex
	events  []public.Event
	err     error
	entered chan struct{}
	release chan struct{}
}

func (s *batchStore) RecordTrafficBatch(ctx context.Context, events []public.Event) error {
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	s.events = append(s.events, events...)
	s.mu.Unlock()
	return nil
}

func validEvent(id string) public.Event {
	return public.Event{At: time.Now().UTC(), RequestID: model.ID(id), ConnectionID: "connection", Host: "example.invalid", Protocol: "http", Action: "proxy"}
}

func TestAsyncDrainsAcceptedEventsOnStop(t *testing.T) {
	store := &batchStore{}
	recorder, err := NewAsync(store, 8, 4, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three"} {
		if err = recorder.Record(t.Context(), validEvent(id)); err != nil {
			t.Fatal(err)
		}
	}
	recorder.Stop()
	stats := recorder.Stats()
	if stats.Accepted != 3 || stats.Written != 3 || stats.Queued != 0 || !stats.Stopped {
		t.Fatal(stats)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 3 {
		t.Fatal(store.events)
	}
	if err = recorder.Record(t.Context(), validEvent("late")); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestAsyncReportsQueueAndWriteLoss(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	store := &batchStore{entered: entered, release: release, err: errors.New("disk unavailable")}
	recorder, err := NewAsync(store, 1, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start()
	if err = recorder.Record(t.Context(), validEvent("writing")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	if err = recorder.Record(t.Context(), validEvent("queued")); err != nil {
		t.Fatal(err)
	}
	if err = recorder.Record(t.Context(), validEvent("overflow")); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	close(release)
	recorder.Stop()
	stats := recorder.Stats()
	if stats.Accepted != 2 || stats.QueueDropped != 1 || stats.Written != 0 || stats.FailedEvents != 2 || stats.WriteFailures != 2 {
		t.Fatal(stats)
	}
}

func TestAsyncRejectsInvalidConfigurationAndEvent(t *testing.T) {
	store := &batchStore{}
	for _, create := range []func() (*Async, error){
		func() (*Async, error) { return NewAsync(nil, 1, 1, time.Second) },
		func() (*Async, error) { return NewAsync(store, 0, 1, time.Second) },
		func() (*Async, error) { return NewAsync(store, 1, 2, time.Second) },
		func() (*Async, error) { return NewAsync(store, 1, 1, 0) },
	} {
		if recorder, err := create(); !errors.Is(err, ErrFull) || recorder != nil {
			t.Fatal(recorder, err)
		}
	}
	recorder, _ := NewAsync(store, 1, 1, time.Second)
	t.Cleanup(recorder.Stop)
	if err := recorder.Record(t.Context(), public.Event{}); !errors.Is(err, public.ErrInvalidEvent) {
		t.Fatal(err)
	}
}
