// Package app composes validated configuration into runtime components.
package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/secrets"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/transport/httpforward"
	"github.com/nguyenduytan/proxysieve/internal/upstream"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
)

var ErrUnsupportedListener = errors.New("configured listener type is not implemented")
var ErrPoolUnavailable = errors.New("no usable proxy route")

type Runtime struct {
	Server *http.Server
	Bind   string
}
type resolver struct{}

func (resolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

type router struct {
	document    policy.Policy
	endpoints   map[model.ID]proxy.Endpoint
	pools       map[model.ID]routing.Pool
	selectors   map[routing.Strategy]*routing.BuiltIn
	resolver    security.Resolver
	destination security.DestinationPolicy
	credentials upstream.CredentialResolver
}

func (r *router) Evaluate(_ context.Context, request policy.RequestContext, visibility policy.Visibility) (policy.Result, error) {
	return policy.Evaluate(r.document, request, visibility, false)
}
func (r *router) Route(ctx context.Context, request policy.RequestContext, result policy.Result) (gateway.Route, error) {
	action := terminal(result.Actions)
	switch action.Type {
	case "direct":
		return r.direct(ctx, request)
	case "proxy":
		return r.proxy(ctx, request, action.PoolID)
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
func (r *router) proxy(ctx context.Context, request policy.RequestContext, poolID model.ID) (gateway.Route, error) {
	endpoint, err := r.selectEndpoint(ctx, poolID, request)
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
	return gateway.Route{Action: "proxy", Transport: transport, Dial: func(ctx context.Context, target string) (net.Conn, error) {
		return connector.Connect(ctx, endpoint, target)
	}}, nil
}
func (r *router) selectEndpoint(ctx context.Context, poolID model.ID, request policy.RequestContext) (proxy.Endpoint, error) {
	seen := map[model.ID]bool{}
	var selectPool func(model.ID) (proxy.Endpoint, error)
	selectPool = func(id model.ID) (proxy.Endpoint, error) {
		if seen[id] {
			return proxy.Endpoint{}, ErrPoolUnavailable
		}
		seen[id] = true
		pool, ok := r.pools[id]
		if !ok || !pool.Enabled {
			return proxy.Endpoint{}, ErrPoolUnavailable
		}
		candidates := make([]routing.Candidate, 0, len(pool.EndpointIDs))
		for _, endpointID := range pool.EndpointIDs {
			endpoint := r.endpoints[endpointID]
			if endpoint.Enabled && matchesPool(endpoint, pool) {
				candidates = append(candidates, routing.Candidate{Endpoint: endpoint, HealthScore: 100})
			}
		}
		if len(candidates) > 0 {
			selector := r.selectors[pool.Strategy]
			chosen, err := selector.Select(ctx, routing.SelectionContext{PoolID: id, Host: request.Host, ClientID: request.ClientID}, candidates)
			if err == nil {
				return r.endpoints[chosen], nil
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
	var listener *config.Listener
	for i := range c.Listeners {
		if c.Listeners[i].Type == "socks5" {
			return Runtime{}, ErrUnsupportedListener
		}
		if c.Listeners[i].Type == "http" {
			if listener != nil {
				return Runtime{}, config.ErrInvalid
			}
			listener = &c.Listeners[i]
		}
	}
	if listener == nil {
		return Runtime{}, ErrUnsupportedListener
	}
	documents := map[model.ID]policy.Policy{}
	for _, document := range c.Policies {
		documents[document.ID] = document.Clone()
	}
	endpoints := map[model.ID]proxy.Endpoint{}
	for _, endpoint := range c.Proxies {
		endpoints[endpoint.ID] = endpoint.Clone()
	}
	pools := map[model.ID]routing.Pool{}
	selectors := map[routing.Strategy]*routing.BuiltIn{}
	for _, pool := range c.Pools {
		pools[pool.ID] = pool.Clone()
		if _, ok := selectors[pool.Strategy]; !ok {
			selector, err := routing.NewBuiltIn(pool.Strategy)
			if err != nil {
				return Runtime{}, err
			}
			selectors[pool.Strategy] = selector
		}
	}
	r := &router{document: documents[model.ID(listener.Policy)], endpoints: endpoints, pools: pools, selectors: selectors, resolver: resolver{}, destination: security.DestinationPolicy{DenyPrivate: c.Security.DenyPrivate}, credentials: secrets.Environment{}}
	handler, err := httpforward.New(httpforward.Options{Evaluator: r, Router: r})
	if err != nil {
		return Runtime{}, err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Duration(listener.IdleTimeout), MaxHeaderBytes: 32 << 10}
	return Runtime{Server: server, Bind: listener.Bind}, nil
}
func (r Runtime) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", r.Bind)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	done := make(chan error, 1)
	go func() { done <- r.Server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = r.Server.Shutdown(shutdownCtx)
		err = <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
