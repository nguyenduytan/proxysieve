package app

import (
	"context"
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
	return api.ProxyHealth{ProxyID: endpoint.ID, Name: endpoint.Name, State: state.State, Circuit: state.Circuit, Score: state.Score, Latency: state.Latency, ConsecutiveFailures: state.ConsecutiveFailures, LastSuccess: nonzeroTime(state.LastSuccess), LastFailure: nonzeroTime(state.LastFailure)}
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
	if err := h.allowTarget(ctx, host); err != nil {
		return api.ProxyHealth{}, err
	}
	snapshot := h.runtime.currentSnapshot()
	endpoint, ok := snapshot.endpoints[id]
	if !ok {
		return api.ProxyHealth{}, store.ErrNotFound
	}
	state, attempted := h.checkEndpoint(ctx, endpoint, "", host, port)
	if !attempted {
		return api.ProxyHealth{}, api.ErrHealthBusy
	}
	return representProxyHealth(endpoint, state), nil
}

func (h *healthControl) CheckPool(ctx context.Context, id model.ID, host string, port uint16) (api.PoolHealth, error) {
	if err := h.allowTarget(ctx, host); err != nil {
		return api.PoolHealth{}, err
	}
	snapshot := h.runtime.currentSnapshot()
	pool, ok := snapshot.pools[id]
	if !ok {
		return api.PoolHealth{}, store.ErrNotFound
	}
	for _, endpointID := range pool.EndpointIDs {
		if ctx.Err() != nil {
			break
		}
		if endpoint := snapshot.endpoints[endpointID]; endpoint.Enabled {
			checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
			h.checkEndpoint(checkCtx, endpoint, id, host, port)
			cancel()
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
		checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
		h.checkEndpoint(checkCtx, endpoint, "", h.targetHost, h.targetPort)
		cancel()
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

func (h *healthControl) checkEndpoint(ctx context.Context, endpoint proxy.Endpoint, poolID model.ID, host string, port uint16) (publichealth.Snapshot, bool) {
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
	if !h.health.Acquire(endpoint.ID, now) {
		state, _ := h.health.EligibleSnapshot(endpoint.ID, now)
		return state, false
	}
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
	connection, err := connector.Connect(ctx, endpoint, net.JoinHostPort(host, strconv.Itoa(int(port))))
	if connection != nil {
		_ = connection.Close()
	}
	state, _ := h.health.Observe(endpoint.ID, publichealth.Observation{Success: err == nil, Timeout: ctx.Err() != nil, Latency: time.Since(started), HealthCheck: true})
	if h.recorder != nil {
		bytes := trafficpkg.Bytes(counter.read.Load() + counter.written.Load())
		status := 200
		if err != nil {
			status = 502
		}
		_ = h.recorder.Record(context.WithoutCancel(ctx), trafficpkg.Event{At: time.Now().UTC(), RequestID: model.NewID(), ConnectionID: model.NewID(), PoolID: poolID, ProxyID: endpoint.ID, Host: host, Protocol: "health", Action: "health_check", StatusCode: status, HealthCheck: bytes})
	}
	return state, true
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
