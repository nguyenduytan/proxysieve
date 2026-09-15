package store

import (
	"context"
	"slices"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
)

// RuntimeBundle is one atomic routing snapshot. It contains references to
// credentials, never raw secret material.
type RuntimeBundle struct {
	Proxies  []proxy.Endpoint `json:"proxies"`
	Pools    []routing.Pool   `json:"pools"`
	Chains   []routing.Chain  `json:"chains"`
	Policies []policy.Policy  `json:"policies"`
}

func (b RuntimeBundle) Clone() RuntimeBundle {
	b.Proxies = slices.Clone(b.Proxies)
	for i := range b.Proxies {
		b.Proxies[i] = b.Proxies[i].Clone()
	}
	b.Pools = slices.Clone(b.Pools)
	for i := range b.Pools {
		b.Pools[i] = b.Pools[i].Clone()
	}
	b.Chains = slices.Clone(b.Chains)
	for i := range b.Chains {
		b.Chains[i] = b.Chains[i].Clone()
	}
	b.Policies = slices.Clone(b.Policies)
	for i := range b.Policies {
		b.Policies[i] = b.Policies[i].Clone()
	}
	return b
}

type RuntimeRecord struct {
	Revision       int64         `json:"revision"`
	Bundle         RuntimeBundle `json:"bundle"`
	ActivatedAt    time.Time     `json:"activated_at"`
	ActivatedBy    model.ID      `json:"activated_by"`
	SourceRevision int64         `json:"source_revision,omitempty"`
}

type RuntimeSnapshots interface {
	LoadRuntimeInventory(context.Context) (RuntimeBundle, error)
	CurrentRuntime(context.Context) (RuntimeRecord, error)
	GetRuntime(context.Context, int64) (RuntimeRecord, error)
	ListRuntime(context.Context, int64, int) ([]RuntimeRecord, error)
	ActivateRuntime(context.Context, RuntimeBundle, int64, model.ID, int64, time.Time) (RuntimeRecord, error)
}
