package routing

import (
	"slices"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

const (
	MaxChainHops      = 8
	DefaultHopTimeout = 15 * time.Second
)

// Chain is an ordered set of proxy pools. Every hop is mandatory: runtime
// failure never falls back to DIRECT or skips a hop.
type Chain struct {
	ID      model.ID `json:"id" yaml:"id"`
	Name    string   `json:"name" yaml:"name"`
	Hops    []Hop    `json:"hops" yaml:"hops"`
	Enabled bool     `json:"enabled" yaml:"enabled"`
}

type Hop struct {
	PoolID  model.ID      `json:"pool_id" yaml:"pool"`
	Timeout time.Duration `json:"timeout_ns" yaml:"timeout"`
}

func (c Chain) Clone() Chain {
	c.Hops = slices.Clone(c.Hops)
	return c
}

func (c Chain) Validate() error {
	if !c.ID.Valid() || strings.TrimSpace(c.Name) == "" || len(c.Name) > 256 || len(c.Hops) < 2 || len(c.Hops) > MaxChainHops {
		return model.ErrInvalid
	}
	seen := map[model.ID]bool{}
	for _, hop := range c.Hops {
		if !hop.PoolID.Valid() || seen[hop.PoolID] || hop.Timeout < 0 || hop.Timeout > 2*time.Minute {
			return model.ErrInvalid
		}
		seen[hop.PoolID] = true
	}
	return nil
}

func (h Hop) EffectiveTimeout() time.Duration {
	if h.Timeout == 0 {
		return DefaultHopTimeout
	}
	return h.Timeout
}
