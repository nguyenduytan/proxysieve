package traffic

import (
	"context"
	"errors"
	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
	"testing"
)

func TestBoundedRecorder(t *testing.T) {
	m, err := NewMemory(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Record(context.Background(), public.Event{}); err != nil {
		t.Fatal(err)
	}
	if err = m.Record(context.Background(), public.Event{}); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	events, dropped := m.Snapshot()
	if len(events) != 1 || dropped != 1 {
		t.Fatal(events, dropped)
	}
}
