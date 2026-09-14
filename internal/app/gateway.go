// Package app composes validated configuration into runtime components.
package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"strconv"
	"strings"
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
	"github.com/nguyenduytan/proxysieve/pkg/routing"
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
	case "block", "reject":
		return gateway.Route{Action: action.Type}, nil
	}
	return gateway.Route{}, gateway.ErrDenied
}
func terminal(actions []policy.Action) policy.Action {
	for _, action := range actions {
		switch action.Type {
		case "block", "reject", "proxy", "direct", "cache", "mock", "redirect", "rewrite":
			return action
		}
	}
	return policy.Action{}
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
	endpoint, err := r.selectEndpoint(ctx, poolID, request, snapshot)
	if err != nil {
		return gateway.Route{}, ErrPoolUnavailable
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
	return gateway.Route{Action: "proxy", PoolID: poolID, ProxyID: endpoint.ID, Rate: rate, Reserve: reserve, Transport: transport, Dial: func(ctx context.Context, target string) (net.Conn, error) {
		return connector.Connect(ctx, endpoint, target)
	}, Observe: func(success bool, status int) {
		_, _ = r.health.Observe(endpoint.ID, publichealth.Observation{Success: success, HTTPStatus: status})
	}}, nil
}
func (r *router) selectEndpoint(ctx context.Context, poolID model.ID, request policy.RequestContext, snapshot *routingSnapshot) (proxy.Endpoint, error) {
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
		for _, endpointID := range pool.EndpointIDs {
			endpoint := snapshot.endpoints[endpointID]
			if endpoint.Enabled && matchesPool(endpoint, pool) && r.health.Eligible(endpoint.ID, time.Now().UTC()) {
				candidates = append(candidates, routing.Candidate{Endpoint: endpoint, HealthScore: 100})
			}
		}
		if len(candidates) > 0 {
			selector := snapshot.selectors[pool.Strategy]
			chosen, err := selector.Select(ctx, routing.SelectionContext{PoolID: id, Host: request.Host, ClientID: request.ClientID}, candidates)
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
		if listener.Auth != "local" && listener.Auth != "api_key" {
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
	configuredBundle := store.RuntimeBundle{Proxies: c.Proxies, Pools: c.Pools, Policies: c.Policies}.Clone()
	trafficRecorder, err := internaltraffic.NewMemory(10_000)
	if err != nil {
		return Runtime{}, err
	}
	var responseCache *internalcache.Memory
	if c.Cache.Response.Enabled {
		responseCache, err = internalcache.NewMemory(c.Cache.Response.MaxEntries, c.Cache.Response.MaxBytes)
		if err != nil {
			return Runtime{}, err
		}
	}
	healthManager, err := internalhealth.New(publichealth.Defaults(), nil)
	if err != nil {
		return Runtime{}, err
	}
	runtime := Runtime{Traffic: trafficRecorder}
	var budgetManager *internalbudget.Manager
	var routeRuntime *routingRuntime
	if c.Admin.Enabled {
		if c.Storage.Driver != "sqlite" {
			return Runtime{}, ErrUnsupportedListener
		}
		controlStore, err := sqlite.Open(context.Background(), c.Storage.Path)
		if err != nil {
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
		durableRecorder, err := internaltraffic.NewAsync(controlStore, 4096, 128, 250*time.Millisecond)
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
		server.SetTrafficStatus(durableRecorder)
		server.SetSourceRefresher(sourceRefresher)
		server.SetRuntimeControl(&runtimeControl{runtime: routeRuntime, store: controlStore, now: func() time.Time { return time.Now().UTC() }})
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
		sourceJob := &scheduler.SourceJob{
			Store: controlStore, Refresher: sourceRefresher, Resolver: resolver{},
			Policy: security.DestinationPolicy{DenyPrivate: true}, Audit: controlStore,
		}
		runtime.Scheduler = scheduler.New(time.Duration(c.Traffic.AggregationInterval), trafficJob.Run, sourceJob.Run)
		if token, err := service.SetupToken(context.Background()); err == nil {
			runtime.SetupToken = token
		}
	}
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
	for _, listener := range c.Listeners {
		if listener.Auth != "local" && listener.Auth != "api_key" {
			return Runtime{}, ErrUnsupportedAuthentication
		}
		r := &router{policyID: model.ID(listener.Policy), runtime: routeRuntime, resolver: resolver{}, destination: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}, credentials: secrets.Environment{}, health: healthManager, directPolicy: c.Security, budgets: budgetManager}
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
			handler, err := httpforward.New(httpforward.Options{Evaluator: r, Router: r, Recorder: recorder, Authenticate: authenticate, ResponseCache: responseCache, MaxCacheBody: min64(c.Cache.Response.MaxBytes, 1<<20)})
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
			server, err := socks5.New(socks5.Options{Evaluator: r, Router: r, Recorder: recorder, IdleTimeout: time.Duration(listener.IdleTimeout)})
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
