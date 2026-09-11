// Package routing defines pool, selection and route decision contracts.
package routing

import (
	"context"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"slices"
	"time"
)

type Strategy string

const (
	Random           Strategy = "random"
	RoundRobin       Strategy = "round-robin"
	WeightedRandom   Strategy = "weighted-random"
	LeastConnections Strategy = "least-connections"
	LeastTraffic     Strategy = "least-traffic"
	LowestLatency    Strategy = "lowest-latency"
	HighestHealth    Strategy = "highest-health"
	LowestCost       Strategy = "lowest-cost"
	CostAware        Strategy = "cost-aware"
	Sticky           Strategy = "sticky"
)

func (s Strategy) Valid() bool {
	switch s {
	case Random, RoundRobin, WeightedRandom, LeastConnections, LeastTraffic, LowestLatency, HighestHealth, LowestCost, CostAware, Sticky:
		return true
	}
	return false
}

type Pool struct {
	ID              model.ID      `json:"id" yaml:"id"`
	Name            string        `json:"name" yaml:"name"`
	Strategy        Strategy      `json:"strategy" yaml:"strategy"`
	EndpointIDs     []model.ID    `json:"endpoint_ids" yaml:"endpoint_ids"`
	FallbackPoolIDs []model.ID    `json:"fallback_pool_ids" yaml:"fallback_pool_ids"`
	RequiredTags    []string      `json:"required_tags" yaml:"required_tags"`
	Country         string        `json:"country" yaml:"country"`
	MinHealthScore  uint8         `json:"min_health_score" yaml:"min_health_score"`
	MaxLatency      time.Duration `json:"max_latency_ns" yaml:"max_latency"`
	Enabled         bool          `json:"enabled" yaml:"enabled"`
}

func (p Pool) Clone() Pool {
	p.EndpointIDs = slices.Clone(p.EndpointIDs)
	p.FallbackPoolIDs = slices.Clone(p.FallbackPoolIDs)
	p.RequiredTags = slices.Clone(p.RequiredTags)
	return p
}

func (p Pool) Validate() error {
	if !p.ID.Valid() || !p.Strategy.Valid() || len(p.Name) > 256 || p.MinHealthScore > 100 || p.MaxLatency < 0 {
		return model.ErrInvalid
	}
	seen := map[model.ID]bool{}
	for _, id := range p.EndpointIDs {
		if !id.Valid() || seen[id] {
			return model.ErrInvalid
		}
		seen[id] = true
	}
	seen = map[model.ID]bool{}
	for _, id := range p.FallbackPoolIDs {
		if !id.Valid() || id == p.ID || seen[id] {
			return model.ErrInvalid
		}
		seen[id] = true
	}
	return nil
}

type SelectionContext struct {
	ClientID, PoolID model.ID
	SessionKey, Host string
}
type Candidate struct {
	Endpoint          proxy.Endpoint
	ActiveConnections uint64
	HealthScore       uint8
	Latency           time.Duration
	Traffic           traffic.Bytes
}
type Selector interface {
	Name() Strategy
	Select(context.Context, SelectionContext, []Candidate) (model.ID, error)
}
type Decision struct {
	ID             model.ID   `json:"id"`
	Action         string     `json:"action"`
	PolicyID       model.ID   `json:"policy_id"`
	RuleIDs        []model.ID `json:"rule_ids"`
	PoolID         model.ID   `json:"pool_id"`
	ProxyID        model.ID   `json:"proxy_id"`
	ChainID        model.ID   `json:"chain_id"`
	ReasonCode     string     `json:"reason_code"`
	ConfigRevision uint64     `json:"config_revision"`
}
