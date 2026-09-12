package traffic

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestEventTimestampAndCounterBounds(t *testing.T) {
	valid := Event{At: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), RequestID: "request", ConnectionID: "connection", UpstreamDownload: Bytes(math.MaxInt64)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{{}, time.Unix(-1, 0), time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)} {
		event := valid
		event.At = at
		if err := event.Validate(); !errors.Is(err, ErrInvalidEvent) {
			t.Fatal(at, err)
		}
	}
	event := valid
	event.UpstreamDownload++
	if err := event.Validate(); !errors.Is(err, ErrInvalidEvent) {
		t.Fatal(err)
	}
	for _, at := range []time.Time{time.Unix(0, 0), time.Unix(0, math.MaxInt64), valid.At.In(time.FixedZone("UTC+7", 7*60*60))} {
		event := valid
		event.At = at
		if err := event.Validate(); err != nil {
			t.Fatal(at, err)
		}
	}
}

func TestEventRejectsMismatchedConfiguredCost(t *testing.T) {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rate := Rate{Price: Money{Currency: "USD", Micros: 1_000_000}, Unit: GB, EffectiveAt: at}
	cost, err := NewCostSnapshot(&rate, 0, 1_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	event := Event{At: at, RequestID: "request", ConnectionID: "connection", Action: "proxy", UpstreamDownload: 1_000_000_000, ConfiguredCost: cost}
	if err = event.Validate(); err != nil {
		t.Fatal(err)
	}
	event.ConfiguredCost.Amount.Micros++
	if err = event.Validate(); !errors.Is(err, ErrInvalidEvent) {
		t.Fatal(err)
	}
}
