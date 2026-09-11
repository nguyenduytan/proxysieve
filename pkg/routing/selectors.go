package routing

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"hash/fnv"
	"math"
	"sync/atomic"
)

var ErrNoCandidate = errors.New("no eligible routing candidate")

type BuiltIn struct {
	strategy Strategy
	counter  atomic.Uint64
}

func NewBuiltIn(strategy Strategy) (*BuiltIn, error) {
	if !strategy.Valid() {
		return nil, ErrNoCandidate
	}
	return &BuiltIn{strategy: strategy}, nil
}
func (s *BuiltIn) Name() Strategy { return s.strategy }
func (s *BuiltIn) Select(ctx context.Context, in SelectionContext, candidates []Candidate) (model.ID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	eligible := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Endpoint.Validate() == nil && c.Endpoint.Enabled {
			eligible = append(eligible, c)
		}
	}
	if len(eligible) == 0 {
		return "", ErrNoCandidate
	}
	switch s.strategy {
	case RoundRobin:
		return eligible[(s.counter.Add(1)-1)%uint64(len(eligible))].Endpoint.ID, nil
	case Random:
		return eligible[randomIndex(uint64(len(eligible)))].Endpoint.ID, nil
	case WeightedRandom:
		return weighted(eligible), nil
	case LeastConnections:
		return pick(eligible, func(a, b Candidate) bool { return a.ActiveConnections < b.ActiveConnections }), nil
	case LeastTraffic:
		return pick(eligible, func(a, b Candidate) bool { return a.Traffic < b.Traffic }), nil
	case LowestLatency:
		return pick(eligible, func(a, b Candidate) bool { return a.Latency < b.Latency }), nil
	case HighestHealth:
		return pick(eligible, func(a, b Candidate) bool { return a.HealthScore > b.HealthScore }), nil
	case LowestCost, CostAware:
		return pick(eligible, func(a, b Candidate) bool { return cost(a) < cost(b) }), nil
	case Sticky:
		return eligible[stableIndex(len(eligible), in)].Endpoint.ID, nil
	}
	return "", ErrNoCandidate
}
func randomIndex(n uint64) uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b[:]) % n
}
func stableIndex(n int, in SelectionContext) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(string(in.ClientID) + "|" + string(in.PoolID) + "|" + in.SessionKey + "|" + in.Host))
	return int(h.Sum64() % uint64(n))
}
func weighted(c []Candidate) model.ID {
	var sum uint64
	for _, x := range c {
		w := uint64(x.Endpoint.Weight)
		if w == 0 {
			w = 1
		}
		if math.MaxUint64-sum < w {
			return c[0].Endpoint.ID
		}
		sum += w
	}
	r := randomIndex(sum)
	for _, x := range c {
		w := uint64(x.Endpoint.Weight)
		if w == 0 {
			w = 1
		}
		if r < w {
			return x.Endpoint.ID
		}
		r -= w
	}
	return c[len(c)-1].Endpoint.ID
}
func pick(c []Candidate, better func(Candidate, Candidate) bool) model.ID {
	best := c[0]
	for _, next := range c[1:] {
		if better(next, best) || !better(best, next) && next.Endpoint.ID < best.Endpoint.ID {
			best = next
		}
	}
	return best.Endpoint.ID
}
func cost(c Candidate) int64 {
	if c.Endpoint.Rate == nil {
		return math.MaxInt64
	}
	return c.Endpoint.Rate.Price.Micros
}
