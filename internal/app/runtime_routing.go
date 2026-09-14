package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type routingSnapshot struct {
	revision  int64
	bundle    store.RuntimeBundle
	documents map[model.ID]policy.Policy
	endpoints map[model.ID]proxy.Endpoint
	pools     map[model.ID]routing.Pool
	selectors map[routing.Strategy]*routing.BuiltIn
}

const retainedRoutingSnapshots int64 = 100

type routingRuntime struct {
	mu      sync.RWMutex
	base    config.Config
	current *routingSnapshot
	history map[int64]*routingSnapshot
	record  store.RuntimeRecord
}

func newRoutingRuntime(base config.Config, bundle store.RuntimeBundle, record store.RuntimeRecord) (*routingRuntime, error) {
	runtime := &routingRuntime{base: base.Clone(), history: map[int64]*routingSnapshot{}, record: record}
	snapshot, err := runtime.compile(bundle, record.Revision)
	if err != nil {
		return nil, err
	}
	runtime.current = snapshot
	runtime.history[snapshot.revision] = snapshot
	runtime.record.Bundle = snapshot.bundle.Clone()
	return runtime, nil
}

func (r *routingRuntime) compile(bundle store.RuntimeBundle, revision int64) (*routingSnapshot, error) {
	candidate := r.base.Clone()
	candidate.Proxies = bundle.Clone().Proxies
	candidate.Pools = bundle.Clone().Pools
	candidate.Policies = bundle.Clone().Policies
	if candidate.Validate() != nil {
		return nil, config.ErrInvalid
	}
	snapshot := &routingSnapshot{
		revision: revision, bundle: bundle.Clone(), documents: map[model.ID]policy.Policy{},
		endpoints: map[model.ID]proxy.Endpoint{}, pools: map[model.ID]routing.Pool{},
		selectors: map[routing.Strategy]*routing.BuiltIn{},
	}
	for _, document := range snapshot.bundle.Policies {
		snapshot.documents[document.ID] = document.Clone()
	}
	for _, endpoint := range snapshot.bundle.Proxies {
		snapshot.endpoints[endpoint.ID] = endpoint.Clone()
	}
	for _, pool := range snapshot.bundle.Pools {
		snapshot.pools[pool.ID] = pool.Clone()
		if _, exists := snapshot.selectors[pool.Strategy]; !exists {
			selector, err := routing.NewBuiltIn(pool.Strategy)
			if err != nil {
				return nil, err
			}
			snapshot.selectors[pool.Strategy] = selector
		}
	}
	return snapshot, nil
}

func (r *routingRuntime) currentSnapshot() *routingSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

func (r *routingRuntime) snapshot(revision int64) (*routingSnapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot, ok := r.history[revision]
	return snapshot, ok
}

func (r *routingRuntime) currentRecord() store.RuntimeRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record := r.record
	record.Bundle = record.Bundle.Clone()
	return record
}

func (r *routingRuntime) publish(snapshot *routingSnapshot, record store.RuntimeRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = snapshot
	r.history[snapshot.revision] = snapshot
	for revision := range r.history {
		if revision <= snapshot.revision-retainedRoutingSnapshots {
			delete(r.history, revision)
		}
	}
	r.record = record
	r.record.Bundle = record.Bundle.Clone()
}

type runtimeInventoryStore interface {
	store.Endpoints
	store.Pools
	store.Policies
	store.RuntimeSnapshots
}

type runtimeControl struct {
	runtime *routingRuntime
	store   runtimeInventoryStore
	now     func() time.Time
	mu      sync.Mutex
}

func (c *runtimeControl) CurrentRuntime() store.RuntimeRecord { return c.runtime.currentRecord() }

func (c *runtimeControl) HasStagedRuntimeChanges(ctx context.Context) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bundle, err := c.store.LoadRuntimeInventory(ctx)
	if err != nil {
		return false, err
	}
	return !reflect.DeepEqual(bundle, c.runtime.currentRecord().Bundle), nil
}

func (c *runtimeControl) ListRuntime(ctx context.Context, before int64, limit int) ([]store.RuntimeRecord, error) {
	return c.store.ListRuntime(ctx, before, limit)
}

func (c *runtimeControl) ActivateRuntime(ctx context.Context, expected int64, actor model.ID) (store.RuntimeRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bundle, err := c.store.LoadRuntimeInventory(ctx)
	if err != nil {
		return store.RuntimeRecord{}, err
	}
	return c.activate(ctx, bundle, expected, actor, 0)
}

func (c *runtimeControl) RollbackRuntime(ctx context.Context, expected, target int64, actor model.ID) (store.RuntimeRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if target < 1 || target == expected {
		return store.RuntimeRecord{}, store.ErrInvalid
	}
	source, err := c.store.GetRuntime(ctx, target)
	if err != nil {
		return store.RuntimeRecord{}, err
	}
	return c.activate(ctx, source.Bundle, expected, actor, target)
}

func (c *runtimeControl) activate(ctx context.Context, bundle store.RuntimeBundle, expected int64, actor model.ID, source int64) (store.RuntimeRecord, error) {
	prepared, err := c.runtime.compile(bundle, expected+1)
	if err != nil {
		return store.RuntimeRecord{}, err
	}
	record, err := c.store.ActivateRuntime(ctx, bundle, expected, actor, source, c.now())
	if err != nil {
		return store.RuntimeRecord{}, err
	}
	if record.Revision != prepared.revision {
		return store.RuntimeRecord{}, store.ErrUnavailable
	}
	c.runtime.publish(prepared, record)
	return record, nil
}

func runtimeRecordOrConfig(ctx context.Context, snapshots store.RuntimeSnapshots, configured store.RuntimeBundle) (store.RuntimeRecord, error) {
	record, err := snapshots.CurrentRuntime(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return store.RuntimeRecord{Revision: 0, Bundle: configured.Clone()}, nil
	}
	return record, err
}
