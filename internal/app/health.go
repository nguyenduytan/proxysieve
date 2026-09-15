package app

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/api"
	internalhealth "github.com/nguyenduytan/proxysieve/internal/health"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/upstream"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type healthControl struct {
	runtime     *routingRuntime
	health      *internalhealth.Manager
	resolver    security.Resolver
	credentials upstream.CredentialResolver
	destination security.DestinationPolicy
	recorder    trafficpkg.Recorder
	targetHost  string
	targetPort  uint16
	timeout     time.Duration
	checks      sync.Map
	rateMu      sync.Mutex
	nextGlobal  time.Time
	nextPools   map[model.ID]time.Time
	globalPace  time.Duration
	poolPace    time.Duration
}

func (h *healthControl) ProxyHealth(ctx context.Context) ([]api.ProxyHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := h.runtime.currentSnapshot()
	now := time.Now().UTC()
	items := make([]api.ProxyHealth, 0, len(snapshot.bundle.Proxies))
	for _, endpoint := range snapshot.bundle.Proxies {
		state, _ := h.health.EligibleSnapshot(endpoint.ID, now)
		if !endpoint.Enabled {
			state.State = publichealth.Disabled
			state.Circuit = publichealth.CircuitOpen
		}
		items = append(items, representProxyHealth(endpoint, state))
	}
	return items, nil
}

func representProxyHealth(endpoint proxy.Endpoint, state publichealth.Snapshot) api.ProxyHealth {
	return api.ProxyHealth{
		ProxyID: endpoint.ID, Name: endpoint.Name, State: state.State, Circuit: state.Circuit,
		Score: state.Score, Latency: state.Latency, Observations: state.Observations,
		Successes: state.Successes, Failures: state.Failures, Timeouts: state.Timeouts,
		AuthFailures: state.AuthFailures, Status403: state.Status403, Status407: state.Status407,
		Status429: state.Status429, Status5xx: state.Status5xx,
		ConsecutiveFailures: state.ConsecutiveFailures,
		LastSuccess:         nonzeroTime(state.LastSuccess), LastFailure: nonzeroTime(state.LastFailure),
	}
}

func nonzeroTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func (h *healthControl) PoolHealth(ctx context.Context) ([]api.PoolHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := h.runtime.currentSnapshot()
	now := time.Now().UTC()
	items := make([]api.PoolHealth, 0, len(snapshot.bundle.Pools))
	for _, pool := range snapshot.bundle.Pools {
		item := api.PoolHealth{PoolID: pool.ID, Name: pool.Name, Enabled: pool.Enabled, Total: len(pool.EndpointIDs)}
		for _, endpointID := range pool.EndpointIDs {
			endpoint := snapshot.endpoints[endpointID]
			state, eligible := h.health.EligibleSnapshot(endpointID, now)
			if !endpoint.Enabled {
				state.State = publichealth.Disabled
				eligible = false
			}
			if pool.Enabled && eligible && endpointEligibleForPool(endpoint, pool, state) {
				item.Eligible++
			}
			switch state.State {
			case publichealth.Healthy:
				item.Healthy++
			case publichealth.Degraded:
				item.Degraded++
			case publichealth.Quarantined:
				item.Quarantined++
			case publichealth.HalfOpen:
				item.HalfOpen++
			case publichealth.Disabled:
				item.Disabled++
			default:
				item.Unknown++
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (h *healthControl) CheckProxy(ctx context.Context, id model.ID, host string, port uint16) (api.ProxyHealth, error) {
	snapshot := h.runtime.currentSnapshot()
	endpoint, ok := snapshot.endpoints[id]
	if !ok {
		return api.ProxyHealth{}, store.ErrNotFound
	}
	if !endpoint.Enabled {
		return api.ProxyHealth{}, api.ErrHealthDisabled
	}
	if err := h.allowTarget(ctx, host); err != nil {
		return api.ProxyHealth{}, err
	}
	state, attempted := h.checkEndpoint(ctx, endpoint, "", endpointPools(snapshot, id), host, port)
	if !attempted {
		if err := ctx.Err(); err != nil {
			return api.ProxyHealth{}, err
		}
		return api.ProxyHealth{}, api.ErrHealthBusy
	}
	return representProxyHealth(endpoint, state), nil
}

func (h *healthControl) CheckPool(ctx context.Context, id model.ID, host string, port uint16) (api.PoolHealth, error) {
	snapshot := h.runtime.currentSnapshot()
	pool, ok := snapshot.pools[id]
	if !ok {
		return api.PoolHealth{}, store.ErrNotFound
	}
	if !pool.Enabled {
		return api.PoolHealth{}, api.ErrHealthDisabled
	}
	if err := h.allowTarget(ctx, host); err != nil {
		return api.PoolHealth{}, err
	}
	for _, endpointID := range pool.EndpointIDs {
		if ctx.Err() != nil {
			break
		}
		if endpoint := snapshot.endpoints[endpointID]; endpoint.Enabled {
			h.checkEndpoint(ctx, endpoint, id, []model.ID{id}, host, port)
		}
	}
	items, err := h.PoolHealth(ctx)
	if err != nil {
		return api.PoolHealth{}, err
	}
	for _, item := range items {
		if item.PoolID == id {
			return item, nil
		}
	}
	return api.PoolHealth{}, store.ErrNotFound
}

func (h *healthControl) Run(ctx context.Context) error {
	if err := h.allowTarget(ctx, h.targetHost); err != nil {
		return err
	}
	snapshot := h.runtime.currentSnapshot()
	for _, endpoint := range snapshot.bundle.Proxies {
		if !endpoint.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		h.checkEndpoint(ctx, endpoint, "", endpointPools(snapshot, endpoint.ID), h.targetHost, h.targetPort)
	}
	return nil
}

func (h *healthControl) allowTarget(ctx context.Context, host string) error {
	if h.resolver == nil {
		return api.ErrHealthTargetDenied
	}
	addresses, err := h.resolver.LookupNetIP(ctx, host)
	if err != nil || h.destination.Allow(addresses) != nil {
		return api.ErrHealthTargetDenied
	}
	return nil
}

func (h *healthControl) checkEndpoint(ctx context.Context, endpoint proxy.Endpoint, poolID model.ID, ratePools []model.ID, host string, port uint16) (publichealth.Snapshot, bool) {
	now := time.Now().UTC()
	if !endpoint.Enabled {
		state, _ := h.health.EligibleSnapshot(endpoint.ID, now)
		return state, false
	}
	if _, loaded := h.checks.LoadOrStore(endpoint.ID, struct{}{}); loaded {
		state, _ := h.health.EligibleSnapshot(endpoint.ID, now)
		return state, false
	}
	defer h.checks.Delete(endpoint.ID)
	if h.waitRate(ctx, ratePools) != nil {
		state, _ := h.health.EligibleSnapshot(endpoint.ID, time.Now().UTC())
		return state, false
	}
	now = time.Now().UTC()
	if !h.health.Acquire(endpoint.ID, now) {
		state, _ := h.health.EligibleSnapshot(endpoint.ID, now)
		return state, false
	}
	checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	started := time.Now()
	counter := &countingConn{}
	connector := upstream.Connector{
		Credentials: h.credentials,
		Resolver:    safeResolver{base: h.resolver, policy: h.destination},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			counter.Conn = connection
			return counter, nil
		},
	}
	connection, err := connector.Connect(checkCtx, endpoint, net.JoinHostPort(host, strconv.Itoa(int(port))))
	if connection != nil {
		_ = connection.Close()
	}
	state, _ := h.health.Observe(endpoint.ID, publichealth.Observation{Success: err == nil, Timeout: checkCtx.Err() != nil, AuthFailure: errors.Is(err, upstream.ErrCredentials), Latency: time.Since(started), HealthCheck: true})
	if h.recorder != nil {
		bytes := trafficpkg.Bytes(counter.read.Load() + counter.written.Load())
		status := 200
		if err != nil {
			status = 502
		}
		_ = h.recorder.Record(context.WithoutCancel(checkCtx), trafficpkg.Event{At: time.Now().UTC(), RequestID: model.NewID(), ConnectionID: model.NewID(), PoolID: poolID, ProxyID: endpoint.ID, Host: host, Protocol: "health", Action: "health_check", StatusCode: status, HealthCheck: bytes})
	}
	return state, true
}

func endpointPools(snapshot *routingSnapshot, endpointID model.ID) []model.ID {
	var ids []model.ID
	for _, pool := range snapshot.bundle.Pools {
		if !pool.Enabled {
			continue
		}
		for _, candidate := range pool.EndpointIDs {
			if candidate == endpointID {
				ids = append(ids, pool.ID)
				break
			}
		}
	}
	return ids
}

func checkPace(perMinute uint32) time.Duration {
	return time.Minute / time.Duration(perMinute)
}

func (h *healthControl) waitRate(ctx context.Context, poolIDs []model.ID) error {
	for {
		delay := h.reserveRate(time.Now(), poolIDs)
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (h *healthControl) reserveRate(now time.Time, poolIDs []model.ID) time.Duration {
	h.rateMu.Lock()
	defer h.rateMu.Unlock()
	if h.nextGlobal.After(now) {
		return h.nextGlobal.Sub(now)
	}
	if h.nextPools == nil {
		h.nextPools = map[model.ID]time.Time{}
	}
	for poolID, next := range h.nextPools {
		if !next.After(now) {
			delete(h.nextPools, poolID)
		}
	}
	for _, poolID := range poolIDs {
		if next := h.nextPools[poolID]; next.After(now) {
			return next.Sub(now)
		}
	}
	h.nextGlobal = now.Add(h.globalPace)
	for _, poolID := range poolIDs {
		if poolID.Valid() {
			h.nextPools[poolID] = now.Add(h.poolPace)
		}
	}
	return 0
}

type countingConn struct {
	net.Conn
	read    atomic.Uint64
	written atomic.Uint64
}

func (c *countingConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	c.read.Add(uint64(n))
	return n, err
}

func (c *countingConn) Write(buffer []byte) (int, error) {
	n, err := c.Conn.Write(buffer)
	c.written.Add(uint64(n))
	return n, err
}
