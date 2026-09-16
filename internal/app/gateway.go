// Package app composes validated configuration into runtime components.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/api"
	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	"github.com/nguyenduytan/proxysieve/internal/downstreamauth"
	internalhealth "github.com/nguyenduytan/proxysieve/internal/health"
	"github.com/nguyenduytan/proxysieve/internal/scheduler"
	"github.com/nguyenduytan/proxysieve/internal/secrets"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsession "github.com/nguyenduytan/proxysieve/internal/session"
	internalsource "github.com/nguyenduytan/proxysieve/internal/source"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/internal/transport/httpforward"
	"github.com/nguyenduytan/proxysieve/internal/transport/socks5"
	"github.com/nguyenduytan/proxysieve/internal/upstream"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	retrypkg "github.com/nguyenduytan/proxysieve/pkg/retry"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrUnsupportedListener = errors.New("configured listener type is not implemented")
var ErrUnsupportedAuthentication = errors.New("configured listener authentication is not implemented")
var ErrPoolUnavailable = errors.New("no usable proxy route")
var ErrUnsupportedAdminTLS = errors.New("admin TLS serving is not implemented")

type Runtime struct {
	Server     *http.Server
	Bind       string
	HTTPMax    int
	SOCKS      *socks5.Server
	SOCKSBind  string
	SOCKSMax   int
	Traffic    *internaltraffic.Memory
	Durable    *internaltraffic.Async
	Admin      *http.Server
	AdminBind  string
	AdminMax   int
	SetupToken string
	Store      *sqlite.Store
	Scheduler  *scheduler.Runner
	HealthJob  *scheduler.Runner
	Sessions   *internalsession.Manager
}
type resolver struct{}

func (resolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

type router struct {
	policyID     model.ID
	runtime      *routingRuntime
	resolver     security.Resolver
	destination  security.DestinationPolicy
	credentials  upstream.CredentialResolver
	health       *internalhealth.Manager
	directPolicy config.Security
	budgets      *internalbudget.Manager
	sessions     *internalsession.Manager
	retryPolicy  retrypkg.Policy
}

func (r *router) Evaluate(_ context.Context, request policy.RequestContext, visibility policy.Visibility) (policy.Result, error) {
	snapshot := r.runtime.currentSnapshot()
	document, ok := snapshot.documents[r.policyID]
	if !ok {
		return policy.Result{}, policy.ErrNoRoute
	}
	result, err := policy.Evaluate(document, request, visibility, false)
	result.RuntimeRevision = snapshot.revision
	return result, err
}
func (r *router) Route(ctx context.Context, request policy.RequestContext, result policy.Result) (gateway.Route, error) {
	action := terminal(result.Actions)
	switch action.Type {
	case "direct":
		allowed := false
		if r.directPolicy.AllowDirect {
			for _, pattern := range r.directPolicy.DirectAllowlist {
				if match, err := path.Match(strings.ToLower(pattern), strings.ToLower(request.Host)); err == nil && match {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			return gateway.Route{}, gateway.ErrDenied
		}
		return r.direct(ctx, request)
	case "proxy":
		if r.runtime == nil {
			return gateway.Route{}, gateway.ErrDenied
		}
		snapshot, ok := r.runtime.snapshot(result.RuntimeRevision)
		if !ok {
			return gateway.Route{}, gateway.ErrDenied
		}
		return r.proxy(ctx, request, action.PoolID, snapshot)
	case "chain":
		if r.runtime == nil {
			return gateway.Route{}, gateway.ErrDenied
		}
		snapshot, ok := r.runtime.snapshot(result.RuntimeRevision)
		if !ok {
			return gateway.Route{}, gateway.ErrDenied
		}
		return r.chainWithFallbacks(ctx, request, action.ChainID, action.FallbackChainIDs, snapshot)
	case "block", "reject":
		return gateway.Route{Action: action.Type}, nil
	case "cache", "mock", "redirect", "rewrite":
		return gateway.Route{Action: action.Type}, gateway.ErrUnsupported
	}
	return gateway.Route{}, gateway.ErrDenied
}
func terminal(actions []policy.Action) policy.Action {
	for _, action := range actions {
		switch action.Type {
		case "block", "reject", "proxy", "chain", "direct", "cache", "mock", "redirect", "rewrite":
			return action
		}
	}
	return policy.Action{}
}

func (r *router) chain(ctx context.Context, request policy.RequestContext, chainID model.ID, snapshot *routingSnapshot) (gateway.Route, error) {
	return r.chainWithFallbacks(ctx, request, chainID, nil, snapshot)
}

func (r *router) chainWithFallbacks(ctx context.Context, request policy.RequestContext, chainID model.ID, fallbacks []model.ID, snapshot *routingSnapshot) (gateway.Route, error) {
	chainIDs := append([]model.ID{chainID}, fallbacks...)
	return r.selectChain(ctx, request, chainIDs, snapshot, map[model.ID]bool{})
}

func (r *router) selectChain(ctx context.Context, request policy.RequestContext, chainIDs []model.ID, snapshot *routingSnapshot, excluded map[model.ID]bool) (gateway.Route, error) {
	for _, chainID := range chainIDs {
		nextExcluded := maps.Clone(excluded)
		route, err := r.buildChain(ctx, request, chainID, snapshot, nextExcluded)
		if err != nil {
			continue
		}
		route.Retry = func(ctx context.Context) (gateway.Route, error) {
			return r.selectChain(ctx, request, chainIDs, snapshot, nextExcluded)
		}
		return route, nil
	}
	return gateway.Route{}, ErrPoolUnavailable
}

func (r *router) buildChain(ctx context.Context, request policy.RequestContext, chainID model.ID, snapshot *routingSnapshot, excluded map[model.ID]bool) (gateway.Route, error) {
	chain, ok := snapshot.chains[chainID]
	if !ok || !chain.Enabled {
		return gateway.Route{}, ErrPoolUnavailable
	}
	hops := make([]upstream.ChainHop, 0, len(chain.Hops))
	pools := make([]model.ID, 0, len(chain.Hops))
	proxies := make([]model.ID, 0, len(chain.Hops))
	for _, configured := range chain.Hops {
		endpoint, err := r.selectEndpointExcluding(ctx, configured.PoolID, request, snapshot, excluded)
		if err != nil || r.destination.DenyPrivate && !endpoint.TrustedRemoteDNS {
			return gateway.Route{}, ErrPoolUnavailable
		}
		excluded[endpoint.ID] = true
		pools = append(pools, configured.PoolID)
		proxies = append(proxies, endpoint.ID)
		hops = append(hops, upstream.ChainHop{PoolID: string(configured.PoolID), Endpoint: endpoint, Timeout: configured.EffectiveTimeout()})
	}
	connector := upstream.Connector{Credentials: r.credentials, Resolver: safeResolver{base: r.resolver, policy: r.destination}}
	var failureMu sync.Mutex
	var failedEndpoint model.ID
	dial := func(ctx context.Context, target string) (net.Conn, error) {
		connection, err := connector.ConnectChain(ctx, hops, target)
		failureMu.Lock()
		failedEndpoint = ""
		var chainErr *upstream.ChainError
		if errors.As(err, &chainErr) {
			failedEndpoint = model.ID(chainErr.EndpointID)
		}
		failureMu.Unlock()
		return connection, err
	}
	transport := &http.Transport{
		Proxy: nil, ForceAttemptHTTP2: false, MaxIdleConns: 8, MaxIdleConnsPerHost: 2,
		IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				return nil, upstream.ErrConnect
			}
			return dial(ctx, address)
		},
	}
	var reserve publicbudget.ReserveFunc
	if r.budgets != nil {
		seenBudget := map[model.ID]bool{}
		ids := make([]model.ID, 0)
		for index, endpointID := range proxies {
			for _, id := range r.budgets.ApplicableIDs(request.ClientID, pools[index], endpointID) {
				if !seenBudget[id] {
					seenBudget[id] = true
					ids = append(ids, id)
				}
			}
		}
		if len(ids) > 0 {
			reserve = func(ctx context.Context, amount trafficpkg.Bytes) (publicbudget.Lease, error) {
				return r.budgets.Reserve(ctx, ids, amount)
			}
		}
	}
	routeStarted := time.Now()
	return gateway.Route{
		Action: "chain", ChainID: chainID, ChainPools: pools, ChainProxies: proxies,
		PoolID: pools[0], ProxyID: proxies[len(proxies)-1], Transport: transport, Dial: dial, Reserve: reserve,
		Acquire: func() bool {
			acquired := make([]model.ID, 0, len(proxies))
			for _, endpointID := range proxies {
				if !r.health.Acquire(endpointID, time.Now().UTC()) {
					for _, claimed := range acquired {
						r.health.Release(claimed)
					}
					return false
				}
				acquired = append(acquired, endpointID)
			}
			return true
		},
		RetryPolicy: &r.retryPolicy,
		Observe: func(observation publichealth.Observation) {
			observation = healthObservation(observation)
			failureMu.Lock()
			endpointID := failedEndpoint
			failureMu.Unlock()
			if observation.Success {
				for _, proxyID := range proxies {
					_, _ = r.health.Observe(proxyID, observation)
				}
				return
			}
			for _, proxyID := range proxies {
				if proxyID == endpointID {
					_, _ = r.health.Observe(proxyID, observation)
				} else {
					r.health.Release(proxyID)
				}
			}
		},
		Complete: func(_ context.Context, upload, download trafficpkg.Bytes) {
			r.recordThroughput(proxies, routeStarted, upload, download)
		},
	}, nil
}

func (r *router) testChain(ctx context.Context, chainID model.ID, host string, port uint16) api.ChainTestResult {
	started := time.Now()
	result := api.ChainTestResult{Status: "unhealthy"}
	route, err := r.chain(ctx, policy.RequestContext{Host: host, Port: port, Timestamp: started.UTC()}, chainID, r.runtime.currentSnapshot())
	if err != nil {
		result.FailureReason = "route_unavailable"
		result.Latency = time.Since(started).Nanoseconds()
		return result
	}
	if route.Acquire != nil && !route.Acquire() {
		result.FailureReason = "route_unavailable"
		result.Latency = time.Since(started).Nanoseconds()
		return result
	}
	dialStarted := time.Now()
	connection, err := route.Dial(ctx, net.JoinHostPort(host, strconv.Itoa(int(port))))
	connectLatency := time.Since(dialStarted)
	result.Latency = time.Since(started).Nanoseconds()
	if err != nil {
		result.FailureReason = "connect_failed"
		var chainErr *upstream.ChainError
		if errors.As(err, &chainErr) {
			result.FailedHop = chainErr.Hop + 1
			result.PoolID = model.ID(chainErr.PoolID)
			result.ProxyID = model.ID(chainErr.EndpointID)
		}
		if route.Observe != nil {
			route.Observe(publichealth.Observation{Success: false, Latency: connectLatency, ConnectLatency: connectLatency, Cause: err})
		}
		return result
	}
	_ = connection.Close()
	result.Status = "healthy"
	if route.Observe != nil {
		route.Observe(publichealth.Observation{Success: true, Latency: connectLatency, ConnectLatency: connectLatency})
	}
	return result
}
func (r *router) direct(ctx context.Context, request policy.RequestContext) (gateway.Route, error) {
	dial, err := r.pinnedDial(ctx, request)
	if err != nil {
		return gateway.Route{}, gateway.ErrDenied
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) { return dial(ctx, address) }}
	return gateway.Route{Action: "direct", Transport: transport, Dial: dial}, nil
}
func (r *router) pinnedDial(ctx context.Context, request policy.RequestContext) (func(context.Context, string) (net.Conn, error), error) {
	addresses, err := r.resolver.LookupNetIP(ctx, request.Host)
	if err != nil || r.destination.Allow(addresses) != nil {
		return nil, gateway.ErrDenied
	}
	return func(ctx context.Context, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != strconv.Itoa(int(request.Port)) || !sameHost(host, request.Host) {
			return nil, gateway.ErrDenied
		}
		var last error
		dialer := net.Dialer{Timeout: 15 * time.Second}
		for _, ip := range addresses {
			conn, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		if last == nil {
			last = gateway.ErrDenied
		}
		return nil, last
	}, nil
}
func (r *router) proxy(ctx context.Context, request policy.RequestContext, poolID model.ID, snapshot *routingSnapshot) (gateway.Route, error) {
	return r.proxyExcluding(ctx, request, poolID, snapshot, nil)
}

func (r *router) proxyExcluding(ctx context.Context, request policy.RequestContext, poolID model.ID, snapshot *routingSnapshot, excluded map[model.ID]bool) (gateway.Route, error) {
	pool, ok := snapshot.pools[poolID]
	if !ok || !pool.Enabled {
		return gateway.Route{}, ErrPoolUnavailable
	}
	sessionPolicy := pool.SessionPolicy.Normalized()
	clientID := request.ClientID
	if !clientID.Valid() {
		clientID = "local"
	}
	var endpoint proxy.Endpoint
	var sessionID model.ID
	var sessionHash string
	var err error
	if r.sessions == nil {
		endpoint, err = r.selectEndpointExcluding(ctx, poolID, request, snapshot, excluded)
		if err != nil {
			return gateway.Route{}, ErrPoolUnavailable
		}
	} else {
		sessionKey, err := routingSessionKey(sessionPolicy.Strategy, clientID, request)
		if err != nil {
			return gateway.Route{}, gateway.ErrDenied
		}
		resolve := func() (internalsession.Result, error) {
			return r.sessions.Resolve(ctx, internalsession.Request{
				ClientID: clientID, PoolID: poolID, Key: sessionKey, Policy: sessionPolicy,
				RuntimeRevision: snapshot.revision,
				Select: func(ctx context.Context) (model.ID, error) {
					endpoint, selectErr := r.selectEndpointExcluding(ctx, poolID, request, snapshot, excluded)
					return endpoint.ID, selectErr
				},
			})
		}
		resolved, err := resolve()
		if err != nil {
			return gateway.Route{}, ErrPoolUnavailable
		}
		if resolved.Reused {
			if reason := r.sessionEndpointRotationReason(resolved.Session.ProxyEndpointID, poolID, snapshot); reason != "" {
				if rotateErr := r.sessions.Rotate(ctx, resolved.Session.ID, reason); rotateErr != nil {
					return gateway.Route{}, ErrPoolUnavailable
				}
				resolved, err = resolve()
				if err != nil {
					return gateway.Route{}, ErrPoolUnavailable
				}
			}
		}
		endpoint, ok = snapshot.endpoints[resolved.Session.ProxyEndpointID]
		if !ok {
			return gateway.Route{}, ErrPoolUnavailable
		}
		if sessionPolicy.Strategy != publicsession.None {
			sessionID, sessionHash = resolved.Session.ID, resolved.Session.KeyHash
		}
	}
	if r.destination.DenyPrivate && !endpoint.TrustedRemoteDNS {
		return gateway.Route{}, gateway.ErrDenied
	}
	credentials, err := r.credentials.ResolveCredentials(ctx, endpoint.CredentialRef)
	if endpoint.CredentialRef != "" && err != nil {
		return gateway.Route{}, gateway.ErrDenied
	}
	connector := upstream.Connector{Credentials: r.credentials, Resolver: safeResolver{base: r.resolver, policy: r.destination}}
	transport, err := upstream.NewHTTPTransport(endpoint, credentials, connector)
	if err != nil {
		return gateway.Route{}, ErrPoolUnavailable
	}
	var rate *trafficpkg.Rate
	if endpoint.Rate != nil && !endpoint.Rate.EffectiveAt.After(request.Timestamp) {
		snapshot := *endpoint.Rate
		rate = &snapshot
	}
	var reserve publicbudget.ReserveFunc
	if r.budgets != nil {
		ids := r.budgets.ApplicableIDs(request.ClientID, poolID, endpoint.ID)
		if len(ids) > 0 {
			reserve = func(ctx context.Context, amount trafficpkg.Bytes) (publicbudget.Lease, error) {
				return r.budgets.Reserve(ctx, ids, amount)
			}
		}
	}
	routeStarted := time.Now()
	route := gateway.Route{Action: "proxy", PoolID: poolID, ProxyID: endpoint.ID, SessionID: sessionID, SessionHash: sessionHash, Rate: rate, Reserve: reserve, Transport: transport, RetryPolicy: &r.retryPolicy, Acquire: func() bool {
		return r.health.Acquire(endpoint.ID, time.Now().UTC())
	}, Retry: func(ctx context.Context) (gateway.Route, error) {
		if sessionID != "" && r.sessions != nil {
			if err := r.sessions.Rotate(ctx, sessionID, publicsession.ProxyFailed); err != nil {
				return gateway.Route{}, ErrPoolUnavailable
			}
		}
		nextExcluded := maps.Clone(excluded)
		if nextExcluded == nil {
			nextExcluded = map[model.ID]bool{}
		}
		nextExcluded[endpoint.ID] = true
		return r.proxyExcluding(ctx, request, poolID, snapshot, nextExcluded)
	}, Dial: func(ctx context.Context, target string) (net.Conn, error) {
		return connector.Connect(ctx, endpoint, target)
	}, Observe: func(observation publichealth.Observation) {
		observation = healthObservation(observation)
		_, _ = r.health.Observe(endpoint.ID, observation)
		if !observation.Success && sessionID != "" && r.sessions != nil && !r.health.Eligible(endpoint.ID, time.Now().UTC()) {
			_ = r.sessions.Rotate(context.Background(), sessionID, publicsession.ProxyFailed)
		}
	}}
	route.Complete = func(ctx context.Context, upload, download trafficpkg.Bytes) {
		r.recordThroughput([]model.ID{endpoint.ID}, routeStarted, upload, download)
		if r.sessions != nil && sessionPolicy.Strategy != publicsession.None {
			_ = r.sessions.RecordUsage(ctx, sessionID, upload, download)
		}
	}
	return route, nil
}

func healthObservation(observation publichealth.Observation) publichealth.Observation {
	timedOut := errors.Is(observation.Cause, context.DeadlineExceeded)
	var networkError net.Error
	var dnsError *net.DNSError
	observation.AuthFailure = observation.AuthFailure || observation.HTTPStatus == http.StatusProxyAuthRequired || errors.Is(observation.Cause, upstream.ErrCredentials)
	observation.Timeout = observation.Timeout || timedOut || errors.As(observation.Cause, &networkError) && networkError.Timeout()
	observation.DNSFailure = observation.DNSFailure || errors.As(observation.Cause, &dnsError)
	observation.TLSFailure = observation.TLSFailure || errors.Is(observation.Cause, upstream.ErrTLS)
	return observation
}

func (r *router) recordThroughput(ids []model.ID, started time.Time, upload, download trafficpkg.Bytes) {
	total, err := upload.Add(download)
	if err != nil || total == 0 {
		return
	}
	for _, id := range ids {
		_ = r.health.RecordThroughput(id, uint64(total), time.Since(started))
	}
}

func routingSessionKey(strategy publicsession.Strategy, clientID model.ID, request policy.RequestContext) (string, error) {
	switch strategy {
	case publicsession.None:
		return "", nil
	case publicsession.Explicit:
		if request.SessionKey == "" {
			return "", internalsession.ErrInvalid
		}
		return request.SessionKey, nil
	case publicsession.Client:
		return string(clientID), nil
	case publicsession.Destination:
		return strings.ToLower(strings.TrimSuffix(request.Host, ".")), nil
	case publicsession.ClientDestination:
		return string(clientID) + "|" + strings.ToLower(strings.TrimSuffix(request.Host, ".")), nil
	default:
		return "", internalsession.ErrInvalid
	}
}

func (r *router) sessionEndpointRotationReason(endpointID, poolID model.ID, snapshot *routingSnapshot) publicsession.RotationReason {
	endpoint, ok := snapshot.endpoints[endpointID]
	if !ok || !endpoint.Enabled {
		return publicsession.PolicyChange
	}
	seen := map[model.ID]bool{}
	var member func(model.ID) bool
	member = func(id model.ID) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		pool, exists := snapshot.pools[id]
		if !exists || !pool.Enabled {
			return false
		}
		for _, candidate := range pool.EndpointIDs {
			if candidate == endpointID && matchesPool(endpoint, pool) {
				return true
			}
		}
		for _, fallback := range pool.FallbackPoolIDs {
			if member(fallback) {
				return true
			}
		}
		return false
	}
	if !member(poolID) {
		return publicsession.PolicyChange
	}
	if !r.health.Eligible(endpointID, time.Now().UTC()) {
		return publicsession.HealthQuarantine
	}
	return ""
}
func (r *router) selectEndpoint(ctx context.Context, poolID model.ID, request policy.RequestContext, snapshot *routingSnapshot) (proxy.Endpoint, error) {
	return r.selectEndpointExcluding(ctx, poolID, request, snapshot, nil)
}

func (r *router) selectEndpointExcluding(ctx context.Context, poolID model.ID, request policy.RequestContext, snapshot *routingSnapshot, excluded map[model.ID]bool) (proxy.Endpoint, error) {
	seen := map[model.ID]bool{}
	var selectPool func(model.ID) (proxy.Endpoint, error)
	selectPool = func(id model.ID) (proxy.Endpoint, error) {
		if seen[id] {
			return proxy.Endpoint{}, ErrPoolUnavailable
		}
		seen[id] = true
		pool, ok := snapshot.pools[id]
		if !ok || !pool.Enabled {
			return proxy.Endpoint{}, ErrPoolUnavailable
		}
		candidates := make([]routing.Candidate, 0, len(pool.EndpointIDs))
		now := time.Now().UTC()
		for _, endpointID := range pool.EndpointIDs {
			endpoint := snapshot.endpoints[endpointID]
			health, eligible := r.health.EligibleSnapshot(endpoint.ID, now)
			if endpoint.Enabled && !excluded[endpoint.ID] && eligible && endpointEligibleForPool(endpoint, pool, health) {
				candidates = append(candidates, routing.Candidate{Endpoint: endpoint, HealthScore: health.Score, Latency: health.Latency, ActiveConnections: health.ActiveConnections})
			}
		}
		if len(candidates) > 0 {
			selector := snapshot.selectors[pool.Strategy]
			chosen, err := selector.Select(ctx, routing.SelectionContext{PoolID: id, Host: request.Host, ClientID: request.ClientID, SessionKey: request.SessionKey}, candidates)
			if err == nil {
				return snapshot.endpoints[chosen], nil
			}
		}
		for _, fallback := range pool.FallbackPoolIDs {
			if endpoint, err := selectPool(fallback); err == nil {
				return endpoint, nil
			}
		}
		return proxy.Endpoint{}, ErrPoolUnavailable
	}
	return selectPool(poolID)
}
func endpointEligibleForPool(endpoint proxy.Endpoint, pool routing.Pool, health publichealth.Snapshot) bool {
	return matchesPool(endpoint, pool) &&
		(health.State == publichealth.Unknown || pool.MinHealthScore == 0 || health.Score >= pool.MinHealthScore) &&
		(health.Latency == 0 || pool.MaxLatency == 0 || health.Latency <= pool.MaxLatency)
}
func matchesPool(endpoint proxy.Endpoint, pool routing.Pool) bool {
	if pool.Country != "" && !strings.EqualFold(endpoint.Country, pool.Country) {
		return false
	}
	tags := map[string]bool{}
	for _, tag := range endpoint.Tags {
		tags[tag] = true
	}
	for _, required := range pool.RequiredTags {
		if !tags[required] {
			return false
		}
	}
	return true
}

type safeResolver struct {
	base   security.Resolver
	policy security.DestinationPolicy
}

func (s safeResolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	addresses, err := s.base.LookupNetIP(ctx, host)
	if err != nil || s.policy.Allow(addresses) != nil {
		return nil, gateway.ErrDenied
	}
	return addresses, nil
}
func sameHost(left, right string) bool {
	if leftIP, err := netip.ParseAddr(left); err == nil {
		if rightIP, e := netip.ParseAddr(right); e == nil {
			return leftIP.Unmap() == rightIP.Unmap()
		}
	}
	return strings.EqualFold(strings.TrimSuffix(left, "."), strings.TrimSuffix(right, "."))
}

func Build(c config.Config) (Runtime, error) {
	if c.Validate() != nil {
		return Runtime{}, config.ErrInvalid
	}
	if c.Admin.Enabled && c.Admin.TLS {
		return Runtime{}, ErrUnsupportedAdminTLS
	}
	types := map[string]bool{}
	for _, listener := range c.Listeners {
		if listener.Auth != "local" && listener.Auth != "api_key" && listener.Auth != "password" {
			return Runtime{}, ErrUnsupportedAuthentication
		}
		if listener.Auth == "api_key" && listener.Type != "http" {
			return Runtime{}, ErrUnsupportedAuthentication
		}
		if types[listener.Type] {
			return Runtime{}, config.ErrInvalid
		}
		types[listener.Type] = true
	}
	if err := os.MkdirAll(c.Server.DataDir, 0700); err != nil {
		return Runtime{}, err
	}
	configuredBundle := store.RuntimeBundle{Proxies: c.Proxies, Pools: c.Pools, Chains: c.Chains, Policies: c.Policies}.Clone()
	trafficRecorder, err := internaltraffic.NewMemory(10_000)
	if err != nil {
		return Runtime{}, err
	}
	var responseCache internalcache.ResponseStore
	if c.Cache.Response.Enabled {
		switch c.Cache.Response.Driver {
		case "memory":
			responseCache, err = internalcache.NewMemory(c.Cache.Response.MaxEntries, c.Cache.Response.MaxBytes)
		case "disk":
			responseCache, err = internalcache.NewDisk(c.Cache.Response.Path, c.Cache.Response.MaxEntries, c.Cache.Response.MaxBytes)
		}
		if err != nil {
			return Runtime{}, err
		}
	}
	var runtimeResolver security.Resolver = resolver{}
	if c.Cache.DNS.Enabled {
		runtimeResolver, err = internalcache.NewDNS(runtimeResolver, c.Cache.DNS.MaxEntries, c.Cache.DNS.MaxBytes, time.Duration(c.Cache.DNS.TTL))
		if err != nil {
			return Runtime{}, err
		}
	}
	healthManager, err := internalhealth.New(c.Health.RuntimeConfig(), nil)
	if err != nil {
		return Runtime{}, err
	}
	var sessionManager *internalsession.Manager
	runtime := Runtime{Traffic: trafficRecorder}
	var budgetManager *internalbudget.Manager
	var routeRuntime *routingRuntime
	var healthService *healthControl
	if c.Admin.Enabled {
		if c.Storage.Driver != "sqlite" {
			return Runtime{}, ErrUnsupportedListener
		}
		controlStore, err := sqlite.Open(context.Background(), c.Storage.Path)
		if err != nil {
			return Runtime{}, err
		}
		durableSessions, loadErr := controlStore.LoadSessions(context.Background())
		if loadErr != nil {
			_ = controlStore.Close()
			return Runtime{}, loadErr
		}
		sessionSecret, secretErr := loadOrCreateSessionKey(c.Server.DataDir, len(durableSessions) == 0)
		if secretErr != nil {
			_ = controlStore.Close()
			return Runtime{}, secretErr
		}
		sessionManager, err = internalsession.NewPersistent(context.Background(), sessionSecret, 10_000, nil, controlStore)
		if err != nil {
			_ = controlStore.Close()
			return Runtime{}, err
		}
		activeRecord, err := runtimeRecordOrConfig(context.Background(), controlStore, configuredBundle)
		if err != nil {
			_ = controlStore.Close()
			return Runtime{}, err
		}
		routeRuntime, err = newRoutingRuntime(c, activeRecord.Bundle, activeRecord)
		if err != nil {
			_ = controlStore.Close()
			return Runtime{}, err
		}
		if len(c.Budgets) > 0 {
			budgetManager, err = internalbudget.NewPersistent(c.Budgets, 64<<10, controlStore)
			if err != nil {
				_ = controlStore.Close()
				return Runtime{}, err
			}
		}
		service, err := admin.New(controlStore, security.DefaultPasswordParams())
		if err != nil {
			_ = controlStore.Close()
			return Runtime{}, err
		}
		durableRecorder, err := internaltraffic.NewAsync(controlStore, c.Traffic.QueueCapacity, c.Traffic.BatchSize, time.Duration(c.Traffic.FlushInterval))
		if err != nil {
			_ = controlStore.Close()
			return Runtime{}, err
		}
		sourceRefresher := internalsource.NewRefresher()
		server, err := api.New(service, trafficRecorder, controlStore, controlStore, controlStore)
		if err != nil {
			_ = controlStore.Close()
			return Runtime{}, err
		}
		healthService = &healthControl{runtime: routeRuntime, health: healthManager, resolver: runtimeResolver, credentials: secrets.Environment{}, destination: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}, recorder: internaltraffic.Fanout{Sinks: []trafficpkg.Recorder{trafficRecorder, durableRecorder}}, targetHost: c.Health.CheckHost, targetPort: c.Health.CheckPort, timeout: time.Duration(c.Health.CheckTimeout), globalPace: checkPace(c.Health.GlobalCheckRate), poolPace: checkPace(c.Health.PoolCheckRate)}
		server.SetTrafficStatus(durableRecorder)
		server.SetSessions(sessionManager)
		server.SetHealth(healthService)
		server.SetBudgets(budgetManager)
		server.SetCache(responseCache)
		server.SetSourceRefresher(sourceRefresher)
		server.SetRuntimeControl(&runtimeControl{runtime: routeRuntime, store: controlStore, now: func() time.Time { return time.Now().UTC() }})
		chainTester := &router{runtime: routeRuntime, resolver: runtimeResolver, destination: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}, credentials: secrets.Environment{}, health: healthManager, directPolicy: c.Security, budgets: budgetManager, sessions: sessionManager, retryPolicy: c.Retry}
		server.SetChainTester(chainTester.testChain)
		runtime.Admin = &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 32 << 10}
		runtime.AdminBind = c.Admin.Bind
		runtime.AdminMax = 32
		runtime.Store = controlStore
		runtime.Durable = durableRecorder
		rawRetention := time.Duration(c.Traffic.RetentionDays) * 24 * time.Hour
		minuteRetention := maxDuration(time.Duration(c.Traffic.MinuteRetentionDays)*24*time.Hour, rawRetention)
		hourRetention := maxDuration(time.Duration(c.Traffic.HourRetentionDays)*24*time.Hour, minuteRetention)
		dayRetention := maxDuration(time.Duration(c.Traffic.DayRetentionDays)*24*time.Hour, hourRetention)
		trafficJob := scheduler.TrafficJob{
			Store: controlStore, Retention: rawRetention, MinuteRetention: minuteRetention,
			HourRetention: hourRetention, DayRetention: dayRetention,
			RetainTiers: func(ctx context.Context, retention scheduler.TierRetention) (scheduler.TierRetentionResult, error) {
				result, err := controlStore.RetainTrafficTiers(ctx, sqlite.TrafficRetention{
					RawBefore: retention.RawBefore, MinuteBefore: retention.MinuteBefore,
					HourBefore: retention.HourBefore, DayBefore: retention.DayBefore,
				})
				return scheduler.TierRetentionResult{RawEvents: result.RawEvents, Minutes: result.Minutes, Hours: result.Hours, Days: result.Days}, err
			},
		}
		// Source refresh resolves uncached so every fetch checks the current addresses at its SSRF boundary.
		sourceJob := &scheduler.SourceJob{
			Store: controlStore, Refresher: sourceRefresher, Resolver: resolver{},
			Policy: security.DestinationPolicy{DenyPrivate: true}, Audit: controlStore,
		}
		runtime.Scheduler = scheduler.New(time.Duration(c.Traffic.AggregationInterval), 30*time.Second, trafficJob.Run, sourceJob.Run)
		if token, err := service.SetupToken(context.Background()); err == nil {
			runtime.SetupToken = token
		}
	} else {
		sessionSecret := make([]byte, 32)
		if _, err = rand.Read(sessionSecret); err != nil {
			return Runtime{}, err
		}
		sessionManager, err = internalsession.New(sessionSecret, 10_000, nil)
		if err != nil {
			return Runtime{}, err
		}
	}
	runtime.Sessions = sessionManager
	if len(c.Budgets) > 0 && budgetManager == nil {
		return Runtime{}, ErrUnsupportedListener
	}
	if routeRuntime == nil {
		var err error
		routeRuntime, err = newRoutingRuntime(c, configuredBundle, store.RuntimeRecord{Revision: 0, Bundle: configuredBundle})
		if err != nil {
			return Runtime{}, err
		}
	}
	if healthService == nil {
		healthService = &healthControl{runtime: routeRuntime, health: healthManager, resolver: runtimeResolver, credentials: secrets.Environment{}, destination: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}, recorder: trafficRecorder, targetHost: c.Health.CheckHost, targetPort: c.Health.CheckPort, timeout: time.Duration(c.Health.CheckTimeout), globalPace: checkPace(c.Health.GlobalCheckRate), poolPace: checkPace(c.Health.PoolCheckRate)}
	}
	if c.Health.ActiveChecks {
		runtime.HealthJob = scheduler.New(time.Duration(c.Health.CheckInterval), 0, healthService.Run)
	}
	for _, listener := range c.Listeners {
		if listener.Auth != "local" && listener.Auth != "api_key" && listener.Auth != "password" {
			return Runtime{}, ErrUnsupportedAuthentication
		}
		r := &router{policyID: model.ID(listener.Policy), runtime: routeRuntime, resolver: runtimeResolver, destination: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}, credentials: secrets.Environment{}, health: healthManager, directPolicy: c.Security, budgets: budgetManager, sessions: sessionManager, retryPolicy: c.Retry}
		var authenticatePassword func(context.Context, string, string) (model.ID, error)
		if listener.Auth == "password" {
			authenticatePassword = passwordAuthenticator(listener.CredentialRef, listener.Name)
		}
		switch listener.Type {
		case "http":
			if runtime.Server != nil {
				return Runtime{}, config.ErrInvalid
			}
			var authenticate func(context.Context, string) (model.ID, error)
			if listener.Auth == "api_key" {
				if runtime.Store == nil {
					return Runtime{}, ErrUnsupportedAuthentication
				}
				authenticate = func(ctx context.Context, header string) (model.ID, error) {
					return downstreamauth.AuthenticateBearer(ctx, header, runtime.Store)
				}
			}
			var recorder trafficpkg.Recorder = trafficRecorder
			if runtime.Durable != nil {
				recorder = internaltraffic.Fanout{Sinks: []trafficpkg.Recorder{trafficRecorder, runtime.Durable}}
			}
			handler, err := httpforward.New(httpforward.Options{Evaluator: r, Router: r, Recorder: recorder, Authenticate: authenticate, AuthenticatePassword: authenticatePassword, ResponseCache: responseCache, MaxCacheBody: min64(c.Cache.Response.MaxBytes, 1<<20)})
			if err != nil {
				return Runtime{}, err
			}
			runtime.Server = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Duration(listener.IdleTimeout), MaxHeaderBytes: 32 << 10}
			runtime.Bind = listener.Bind
			runtime.HTTPMax = listener.MaxConnections
		case "socks5":
			if runtime.SOCKS != nil {
				return Runtime{}, config.ErrInvalid
			}
			var recorder trafficpkg.Recorder = trafficRecorder
			if runtime.Durable != nil {
				recorder = internaltraffic.Fanout{Sinks: []trafficpkg.Recorder{trafficRecorder, runtime.Durable}}
			}
			server, err := socks5.New(socks5.Options{Evaluator: r, Router: r, Recorder: recorder, AuthenticatePassword: authenticatePassword, IdleTimeout: time.Duration(listener.IdleTimeout)})
			if err != nil {
				return Runtime{}, err
			}
			runtime.SOCKS = server
			runtime.SOCKSBind = listener.Bind
			runtime.SOCKSMax = listener.MaxConnections
		default:
			return Runtime{}, ErrUnsupportedListener
		}
	}
	if runtime.Server == nil && runtime.SOCKS == nil {
		return Runtime{}, ErrUnsupportedListener
	}
	return runtime, nil
}

func loadOrCreateSessionKey(dataDir string, allowCreate bool) ([]byte, error) {
	keyPath := path.Join(dataDir, "session-hmac.key")
	read := func() ([]byte, error) {
		info, err := os.Lstat(keyPath)
		if err != nil || !info.Mode().IsRegular() || goruntime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return nil, internalsession.ErrInvalid
		}
		key, err := os.ReadFile(keyPath)
		if err != nil || len(key) != 32 {
			return nil, internalsession.ErrInvalid
		}
		return key, nil
	}
	if _, err := os.Lstat(keyPath); err == nil {
		return read()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if !allowCreate {
		return nil, internalsession.ErrInvalid
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return read()
	}
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(keyPath)
		}
	}()
	if _, err = file.Write(key); err != nil || file.Sync() != nil || file.Close() != nil {
		return nil, internalsession.ErrInvalid
	}
	ok = true
	return key, nil
}

func passwordAuthenticator(ref secret.Ref, listenerName string) func(context.Context, string, string) (model.ID, error) {
	return func(ctx context.Context, username, password string) (model.ID, error) {
		if len(username) == 0 || len(username) > 255 || len(password) == 0 || len(password) > 255 {
			return "", downstreamauth.ErrUnauthorized
		}
		credentials, err := (secrets.Environment{}).ResolveCredentials(ctx, ref)
		if err != nil {
			return "", downstreamauth.ErrUnauthorized
		}
		expectedUser := credentials.Username.Reveal()
		expectedPassword := credentials.Password.Reveal()
		providedUser, providedPassword := []byte(username), []byte(password)
		userOK := subtle.ConstantTimeCompare(expectedUser, providedUser)
		passwordOK := subtle.ConstantTimeCompare(expectedPassword, providedPassword)
		for i := range expectedUser {
			expectedUser[i] = 0
		}
		for i := range expectedPassword {
			expectedPassword[i] = 0
		}
		for i := range providedUser {
			providedUser[i] = 0
		}
		for i := range providedPassword {
			providedPassword[i] = 0
		}
		if userOK != 1 || passwordOK != 1 {
			return "", downstreamauth.ErrUnauthorized
		}
		hash := sha256.Sum256([]byte(listenerName + "\x00" + username))
		return model.ID("pwd_" + hex.EncodeToString(hash[:])), nil
	}
}
func (r Runtime) Run(ctx context.Context) error {
	return r.RunReady(ctx, nil)
}

// RunReady reports readiness only after all requested ports have been bound.
func (r Runtime) RunReady(ctx context.Context, ready func() error) error {
	if r.Store != nil {
		defer func() { _ = r.Store.Close() }()
	}
	if r.Durable != nil {
		r.Durable.Start()
		defer r.Durable.Stop()
	}
	if r.Scheduler != nil {
		r.Scheduler.Start(ctx)
		defer r.Scheduler.Stop()
	}
	if r.HealthJob != nil {
		r.HealthJob.Start(ctx)
		defer r.HealthJob.Stop()
	}
	if ctx.Err() != nil {
		return nil
	}
	listeners := make([]net.Listener, 0, 3)
	closeAll := func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}
	if r.Server != nil {
		base, err := net.Listen("tcp", r.Bind)
		if err != nil {
			return err
		}
		listener, err := security.NewLimitedListener(base, r.HTTPMax)
		if err != nil {
			_ = base.Close()
			return err
		}
		listeners = append(listeners, listener)
	}
	if r.SOCKS != nil {
		base, err := net.Listen("tcp", r.SOCKSBind)
		if err != nil {
			closeAll()
			return err
		}
		listener, err := security.NewLimitedListener(base, r.SOCKSMax)
		if err != nil {
			_ = base.Close()
			closeAll()
			return err
		}
		listeners = append(listeners, listener)
	}
	if r.Admin != nil {
		base, err := net.Listen("tcp", r.AdminBind)
		if err != nil {
			closeAll()
			return err
		}
		listener, err := security.NewLimitedListener(base, r.AdminMax)
		if err != nil {
			_ = base.Close()
			closeAll()
			return err
		}
		listeners = append(listeners, listener)
	}
	if ready != nil {
		if err := ready(); err != nil {
			closeAll()
			return err
		}
	}
	done := make(chan error, len(listeners))
	if r.Server != nil {
		listener := listeners[0]
		go func() { done <- r.Server.Serve(listener) }()
	}
	if r.SOCKS != nil {
		index := 0
		if r.Server != nil {
			index = 1
		}
		listener := listeners[index]
		go func() { done <- r.serveSOCKS(ctx, listener) }()
	}
	if r.Admin != nil {
		index := 0
		if r.Server != nil {
			index++
		}
		if r.SOCKS != nil {
			index++
		}
		listener := listeners[index]
		go func() { done <- r.Admin.Serve(listener) }()
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if r.Server != nil {
			_ = r.Server.Shutdown(shutdownCtx)
		}
		if r.Admin != nil {
			_ = r.Admin.Shutdown(shutdownCtx)
		}
		closeAll()
		for range cap(done) {
			err := <-done
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				return err
			}
		}
		return nil
	case err := <-done:
		closeAll()
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}
func (r Runtime) serveSOCKS(ctx context.Context, listener net.Listener) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		go r.SOCKS.Serve(ctx, connection)
	}
}
func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxDuration(left, right time.Duration) time.Duration {
	if left < right {
		return right
	}
	return left
}
