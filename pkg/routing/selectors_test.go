package routing

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"testing"
	"time"
)

func candidates() []Candidate {
	return []Candidate{{Endpoint: proxy.Endpoint{ID: "a", Name: "a", Protocol: proxy.HTTP, Host: "a.example.invalid", Port: 8080, Enabled: true, Weight: 1, Rate: &traffic.Rate{Price: traffic.Money{Currency: "USD", Micros: 10}, Unit: traffic.GB, EffectiveAt: time.Unix(0, 0)}}, ActiveConnections: 3, Traffic: 30, HealthScore: 10, Latency: 30 * time.Millisecond}, {Endpoint: proxy.Endpoint{ID: "b", Name: "b", Protocol: proxy.HTTP, Host: "b.example.invalid", Port: 8080, Enabled: true, Weight: 1, Rate: &traffic.Rate{Price: traffic.Money{Currency: "USD", Micros: 5}, Unit: traffic.GB, EffectiveAt: time.Unix(0, 0)}}, ActiveConnections: 1, Traffic: 10, HealthScore: 90, Latency: 10 * time.Millisecond}}
}
func TestBuiltInDeterministicStrategies(t *testing.T) {
	want := map[Strategy]model.ID{RoundRobin: "a", LeastConnections: "b", LeastTraffic: "b", LowestLatency: "b", HighestHealth: "b", LowestCost: "b", CostAware: "b"}
	for strategy, id := range want {
		s, err := NewBuiltIn(strategy)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Select(context.Background(), SelectionContext{}, candidates())
		if err != nil || got != id {
			t.Fatalf("%s %s %v", strategy, got, err)
		}
	}
}
func TestStickyAndFailures(t *testing.T) {
	s, _ := NewBuiltIn(Sticky)
	in := SelectionContext{ClientID: "client", PoolID: "pool", SessionKey: "session", Host: "host"}
	first, err := s.Select(context.Background(), in, candidates())
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		got, _ := s.Select(context.Background(), in, candidates())
		if got != first {
			t.Fatal("sticky changed")
		}
	}
	if _, err = NewBuiltIn("bad"); !errors.Is(err, ErrNoCandidate) {
		t.Fatal(err)
	}
	if _, err = s.Select(context.Background(), in, nil); !errors.Is(err, ErrNoCandidate) {
		t.Fatal(err)
	}
	c := candidates()
	c[0].Endpoint.Enabled = false
	c[1].Endpoint.Enabled = false
	if _, err = s.Select(context.Background(), in, c); !errors.Is(err, ErrNoCandidate) {
		t.Fatal(err)
	}
}
