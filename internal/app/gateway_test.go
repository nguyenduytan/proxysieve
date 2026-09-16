package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsession "github.com/nguyenduytan/proxysieve/internal/session"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/internal/upstream"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

func TestBuildSafety(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Admin.Enabled = false
	r, err := Build(c)
	if err != nil || r.Bind != "127.0.0.1:8080" {
		t.Fatal(r, err)
	}
	c.Listeners = append(c.Listeners, config.Listener{Name: "socks", Type: "socks5", Bind: "127.0.0.1:1080", Auth: "local", Policy: "default", MaxConnections: 1, IdleTimeout: config.Duration(1)})
	r, err = Build(c)
	if err != nil || r.SOCKS == nil || r.SOCKSBind != "127.0.0.1:1080" {
		t.Fatal(r, err)
	}
	c.Listeners[1].Auth = "password"
	c.Listeners[1].CredentialRef = secret.Ref("secret://client/test")
	if _, err = Build(c); err != nil {
		t.Fatal("password listener should be supported", err)
	}
	c = config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[1:]
	c.Listeners[0].Auth = "api_key"
	if _, err = Build(c); !errors.Is(err, ErrUnsupportedAuthentication) {
		t.Fatal(err)
	}
	c = config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Security.AllowDirect = true
	c.Security.DirectAllowlist = []string{"*.example.invalid"}
	if _, err = Build(c); err != nil {
		t.Fatal(err)
	}
	c.Admin.Enabled = true
	c.Admin.TLS = true
	c.Admin.CertFile = "certificate.pem"
	c.Admin.KeyRef = "secret://admin/key"
	if _, err = Build(c); !errors.Is(err, ErrUnsupportedAdminTLS) {
		t.Fatal(err)
	}
}

func TestBuildCreatesConfiguredDiskResponseCache(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Admin.Enabled = false
	c.Listeners = c.Listeners[:1]
	c.Cache.Response.Enabled = true
	c.Cache.Response.Driver = "disk"
	if _, err := Build(c); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(c.Cache.Response.Path); err != nil || !info.IsDir() {
		t.Fatal("disk response cache directory missing", info, err)
	}
}
func TestRunCancelled(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Listeners[0].Bind = freeBind(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCreatesActiveHealthJobWithoutAdmin(t *testing.T) {
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Health.ActiveChecks = true
	runtime, err := Build(c)
	if err != nil || runtime.HealthJob == nil {
		t.Fatal(runtime.HealthJob, err)
	}
}

func TestHealthObservationClassifiesProxyAuthAndTimeout(t *testing.T) {
	observation := healthObservation(publichealth.Observation{
		HTTPStatus: http.StatusProxyAuthRequired,
		Latency:    time.Second,
		Cause:      errors.Join(context.DeadlineExceeded, &net.DNSError{Err: "lookup failed"}, upstream.ErrTLS),
	})
	if !observation.AuthFailure || !observation.Timeout || !observation.DNSFailure || !observation.TLSFailure || observation.HTTPStatus != http.StatusProxyAuthRequired || observation.Latency != time.Second {
		t.Fatal(observation)
	}
}

type fixedResolver struct{ addresses []netip.Addr }

func (r fixedResolver) LookupNetIP(context.Context, string) ([]netip.Addr, error) {
	return r.addresses, nil
}

func TestDirectUsesPinnedResolution(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "pinned") }))
	defer target.Close()
	_, port, _ := net.SplitHostPort(target.Listener.Addr().String())
	r := &router{resolver: fixedResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}, destination: security.DestinationPolicy{AllowTrusted: true}}
	request := policy.RequestContext{Host: "origin.example.invalid", Port: parsePort(t, port)}
	route, err := r.direct(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := route.Transport.RoundTrip(httptest.NewRequest(http.MethodGet, "http://origin.example.invalid:"+port+"/", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "pinned" {
		t.Fatal(string(body))
	}
}
func TestProxyPoolRoutesToUpstream(t *testing.T) {
	var gotURL string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = io.WriteString(w, "proxied")
	}))
	defer upstreamServer.Close()
	upstreamURL, _ := url.Parse(upstreamServer.URL)
	host, portRaw, _ := net.SplitHostPort(upstreamURL.Host)
	port := parsePort(t, portRaw)
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Proxies = []proxy.Endpoint{{ID: "upstream", Name: "upstream", Protocol: proxy.HTTP, Host: host, Port: port, Enabled: true, TrustedRemoteDNS: true}}
	c.Pools = []routing.Pool{{ID: "pool", Name: "pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"upstream"}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	downstreamURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
	response, err := client.Get("http://origin.example.invalid/path")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "proxied" || gotURL != "http://origin.example.invalid/path" {
		t.Fatalf("%q %q", body, gotURL)
	}
}

func TestProxyPoolRetriesOnlySafeBodylessRequests(t *testing.T) {
	for _, test := range []struct {
		method     string
		body       string
		wantStatus int
		wantHits   int
	}{{http.MethodGet, "", http.StatusOK, 1}, {http.MethodPost, "do-not-replay", http.StatusBadGateway, 0}} {
		t.Run(test.method, func(t *testing.T) {
			var hits int
			working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits++
				_, _ = io.WriteString(w, "ok")
			}))
			defer working.Close()
			workingURL, _ := url.Parse(working.URL)
			workingHost, workingPort, _ := net.SplitHostPort(workingURL.Host)
			failedHost, failedPort, _ := net.SplitHostPort(freeBind(t))
			c := config.Defaults(t.TempDir())
			c.Listeners = c.Listeners[:1]
			c.Admin.Enabled = false
			c.Proxies = []proxy.Endpoint{
				{ID: "failed", Name: "Failed", Protocol: proxy.HTTP, Host: failedHost, Port: parsePort(t, failedPort), Enabled: true, TrustedRemoteDNS: true},
				{ID: "working", Name: "Working", Protocol: proxy.HTTP, Host: workingHost, Port: parsePort(t, workingPort), Enabled: true, TrustedRemoteDNS: true},
			}
			c.Pools = []routing.Pool{{ID: "pool", Name: "Pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"failed", "working"}, Enabled: true}}
			c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "Default", Rules: []policy.Rule{{ID: "route", Name: "Route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
			runtime, err := Build(c)
			if err != nil {
				t.Fatal(err)
			}
			downstream := httptest.NewServer(runtime.Server.Handler)
			defer downstream.Close()
			downstreamURL, _ := url.Parse(downstream.URL)
			client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
			request, _ := http.NewRequest(test.method, "http://origin.example.invalid/path", strings.NewReader(test.body))
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != test.wantStatus || hits != test.wantHits {
				t.Fatal(response.StatusCode, hits)
			}
		})
	}
}

func TestProxyChainRoutesThroughEveryHop(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "through-chain")
	}))
	defer target.Close()
	secondTargets := make(chan string, 1)
	second := newConnectProxy(t, secondTargets)
	defer second.Close()
	firstTargets := make(chan string, 1)
	first := newConnectProxy(t, firstTargets)
	defer first.Close()
	endpoint := func(id model.ID, rawURL string) proxy.Endpoint {
		parsed, _ := url.Parse(rawURL)
		host, portRaw, _ := net.SplitHostPort(parsed.Host)
		return proxy.Endpoint{ID: id, Name: string(id), Protocol: proxy.HTTP, Host: host, Port: parsePort(t, portRaw), Enabled: true, TrustedRemoteDNS: true}
	}
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Proxies = []proxy.Endpoint{endpoint("first-proxy", first.URL), endpoint("second-proxy", second.URL)}
	c.Pools = []routing.Pool{
		{ID: "first-pool", Name: "First", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"first-proxy"}, Enabled: true},
		{ID: "second-pool", Name: "Second", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"second-proxy"}, Enabled: true},
	}
	c.Chains = []routing.Chain{{ID: "ordered-chain", Name: "Ordered chain", Hops: []routing.Hop{{PoolID: "first-pool", Timeout: time.Second}, {PoolID: "second-pool", Timeout: time.Second}}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "chain", ChainID: "ordered-chain"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	downstreamURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
	response, err := client.Get(target.URL + "/chain")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "through-chain" {
		t.Fatal(string(body))
	}
	secondURL, _ := url.Parse(second.URL)
	targetURL, _ := url.Parse(target.URL)
	if got := <-firstTargets; got != secondURL.Host {
		t.Fatalf("first hop target=%q want=%q", got, secondURL.Host)
	}
	if got := <-secondTargets; got != targetURL.Host {
		t.Fatalf("second hop target=%q want=%q", got, targetURL.Host)
	}
	events, _ := runtime.Traffic.Snapshot()
	if len(events) != 1 || events[0].Action != "chain" || events[0].ChainID != "ordered-chain" || events[0].PoolID != "first-pool" || events[0].ProxyID != "second-proxy" {
		t.Fatalf("unexpected chain traffic event: %+v", events)
	}
}

func TestProxyChainFallsBackToAlternativeChain(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "through-fallback")
	}))
	defer target.Close()
	second := newConnectProxy(t, make(chan string, 1))
	defer second.Close()
	first := newConnectProxy(t, make(chan string, 1))
	defer first.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer dead.Close()
	endpoint := func(id model.ID, rawAddress string) proxy.Endpoint {
		host, portRaw, _ := net.SplitHostPort(strings.TrimPrefix(rawAddress, "http://"))
		return proxy.Endpoint{ID: id, Name: string(id), Protocol: proxy.HTTP, Host: host, Port: parsePort(t, portRaw), Enabled: true, TrustedRemoteDNS: true}
	}
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Proxies = []proxy.Endpoint{
		endpoint("dead-first", dead.URL), endpoint("dead-second", dead.URL),
		endpoint("live-first", first.URL), endpoint("live-second", second.URL),
	}
	c.Pools = []routing.Pool{
		{ID: "dead-first-pool", Name: "Dead first", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"dead-first"}, Enabled: true},
		{ID: "dead-second-pool", Name: "Dead second", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"dead-second"}, Enabled: true},
		{ID: "live-first-pool", Name: "Live first", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"live-first"}, Enabled: true},
		{ID: "live-second-pool", Name: "Live second", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"live-second"}, Enabled: true},
	}
	c.Chains = []routing.Chain{
		{ID: "primary-chain", Name: "Primary", Hops: []routing.Hop{{PoolID: "dead-first-pool"}, {PoolID: "dead-second-pool"}}, Enabled: true},
		{ID: "fallback-chain", Name: "Fallback", Hops: []routing.Hop{{PoolID: "live-first-pool"}, {PoolID: "live-second-pool"}}, Enabled: true},
	}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "chain", ChainID: "primary-chain", FallbackChainIDs: []model.ID{"fallback-chain"}}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	downstreamURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
	response, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "through-fallback" {
		t.Fatal(string(body))
	}
	events, _ := runtime.Traffic.Snapshot()
	if len(events) != 2 || events[0].ChainID != "primary-chain" || events[0].StatusCode != http.StatusBadGateway || events[1].ChainID != "fallback-chain" || events[1].StatusCode != http.StatusOK {
		t.Fatalf("unexpected failover events: %+v", events)
	}
}

func newConnectProxy(t *testing.T, targets chan<- string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			http.Error(w, "connect failed", http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = upstream.Close()
			t.Error("hijacking unavailable")
			return
		}
		client, buffer, err := hijacker.Hijack()
		if err != nil {
			_ = upstream.Close()
			t.Error(err)
			return
		}
		targets <- r.Host
		if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err == nil {
			err = buffer.Flush()
		}
		if err != nil {
			_ = client.Close()
			_ = upstream.Close()
			return
		}
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close(); done <- struct{}{} }()
		go func() { _, _ = io.Copy(client, upstream); _ = client.Close(); done <- struct{}{} }()
		<-done
		<-done
	}))
}

func TestExplicitSessionAffinityReusesSelectedProxy(t *testing.T) {
	var firstHits, secondHits int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits++
		_, _ = io.WriteString(w, "first")
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondHits++
		_, _ = io.WriteString(w, "second")
	}))
	defer second.Close()
	endpoint := func(id model.ID, rawURL string) proxy.Endpoint {
		parsed, _ := url.Parse(rawURL)
		host, portRaw, _ := net.SplitHostPort(parsed.Host)
		return proxy.Endpoint{ID: id, Name: string(id), Protocol: proxy.HTTP, Host: host, Port: parsePort(t, portRaw), Enabled: true, TrustedRemoteDNS: true}
	}
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Admin.Enabled = false
	c.Proxies = []proxy.Endpoint{endpoint("first", first.URL), endpoint("second", second.URL)}
	c.Pools = []routing.Pool{{ID: "pool", Name: "pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"first", "second"}, SessionPolicy: publicsession.Policy{Strategy: publicsession.Explicit, TTL: time.Hour}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	downstreamURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
	request := func(key string) string {
		req, requestErr := http.NewRequest(http.MethodGet, "http://origin.example.invalid/path", nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		req.Header.Set("X-ProxySieve-Session", key)
		response, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, requestErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return string(body)
	}
	if got := request("alpha"); got != "first" {
		t.Fatal(got)
	}
	if got := request("alpha"); got != "first" {
		t.Fatal("sticky session changed proxy", got)
	}
	if got := request("beta"); got != "second" {
		t.Fatal("independent session did not select independently", got)
	}
	if firstHits != 2 || secondHits != 1 {
		t.Fatal(firstHits, secondHits)
	}
	sessions, err := runtime.Sessions.List(t.Context())
	if err != nil || len(sessions) != 2 || sessions[0].RequestCount+sessions[1].RequestCount != 3 {
		t.Fatal(sessions, err)
	}
}

func TestSQLiteSessionAffinitySurvivesBuildRestart(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults(dir)
	c.Listeners = c.Listeners[:1]
	first, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	request := internalsession.Request{ClientID: "client", PoolID: "pool", Key: "private-key", RuntimeRevision: 1, Policy: publicsession.Policy{Strategy: publicsession.Explicit, TTL: time.Hour}, Select: func(context.Context) (model.ID, error) { return "proxy", nil }}
	created, err := first.Sessions.Resolve(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Store.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := second.Sessions.Resolve(t.Context(), request)
	if err != nil || !reused.Reused || reused.Session.ID != created.Session.ID {
		t.Fatalf("binding did not survive restart: %+v %v", reused, err)
	}
	keyInfo, err := os.Stat(path.Join(c.Server.DataDir, "session-hmac.key"))
	if err != nil || !keyInfo.Mode().IsRegular() || keyInfo.Size() != 32 {
		t.Fatalf("session key file invalid: %+v %v", keyInfo, err)
	}
	if err = second.Store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path.Join(c.Server.DataDir, "session-hmac.key")); err != nil {
		t.Fatal(err)
	}
	if orphaned, buildErr := Build(c); buildErr == nil {
		_ = orphaned.Store.Close()
		t.Fatal("database sessions were accepted without their HMAC key")
	}
}

func TestPaidRouteHardBudgetPersistsAndRejectsNextRequest(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "x")
	}))
	defer upstreamServer.Close()
	upstreamURL, _ := url.Parse(upstreamServer.URL)
	host, portRaw, _ := net.SplitHostPort(upstreamURL.Host)
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Proxies = []proxy.Endpoint{{ID: "upstream", Name: "upstream", Protocol: proxy.HTTP, Host: host, Port: parsePort(t, portRaw), Enabled: true, TrustedRemoteDNS: true}}
	c.Pools = []routing.Pool{{ID: "pool", Name: "pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"upstream"}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
	c.Budgets = []publicbudget.Config{{ID: "system", Name: "System", Scope: publicbudget.ScopeSystem, Limit: 2, Hard: true, Action: publicbudget.ActionReject}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Store.Close() })
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	downstreamURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
	response, err := client.Get("http://origin.example.invalid/first")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(body) != "x" {
		t.Fatal(string(body), err)
	}
	at := time.Now().UTC()
	if err = runtime.Store.ReserveBudgets(t.Context(), c.Budgets, at, 1); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Store.ConsumeBudgets(t.Context(), c.Budgets, at, 1); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get("http://origin.example.invalid/second")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatal(response.StatusCode)
	}
	usage, err := runtime.Store.BudgetUsage(t.Context(), c.Budgets[0], at)
	if err != nil || usage.Used != 2 || usage.Reserved != 0 {
		t.Fatal(usage, err)
	}
}

func TestHTTPEventsArePersistedOffRequestPath(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "persisted")
	}))
	defer upstreamServer.Close()
	upstreamURL, _ := url.Parse(upstreamServer.URL)
	host, portRaw, _ := net.SplitHostPort(upstreamURL.Host)
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[:1]
	c.Proxies = []proxy.Endpoint{{ID: "upstream", Name: "upstream", Protocol: proxy.HTTP, Host: host, Port: parsePort(t, portRaw), Enabled: true, TrustedRemoteDNS: true}}
	c.Pools = []routing.Pool{{ID: "pool", Name: "pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"upstream"}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Durable.Start()
	t.Cleanup(func() {
		runtime.Durable.Stop()
		_ = runtime.Store.Close()
	})
	downstream := httptest.NewServer(runtime.Server.Handler)
	defer downstream.Close()
	downstreamURL, _ := url.Parse(downstream.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(downstreamURL)}}
	response, err := client.Get("http://origin.example.invalid/persist")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	runtime.Durable.Stop()
	events, err := runtime.Store.ListTraffic(t.Context(), sqlite.TrafficPage{Limit: 10})
	if err != nil || len(events) != 1 || events[0].Host != "origin.example.invalid" || events[0].UpstreamDownload == 0 {
		t.Fatal(events, err)
	}
	stats := runtime.Durable.Stats()
	if stats.Accepted != 1 || stats.Written != 1 || stats.QueueDropped != 0 || stats.FailedEvents != 0 {
		t.Fatal(stats)
	}
}

func TestSOCKSTunnelEventsArePersisted(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	targetDone := make(chan struct{})
	go func() {
		defer close(targetDone)
		conn, acceptErr := target.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		payload := make([]byte, 4)
		if _, readErr := io.ReadFull(conn, payload); readErr == nil && string(payload) == "ping" {
			_, _ = conn.Write([]byte("ok"))
		}
	}()
	host, portRaw, err := net.SplitHostPort(target.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port := parsePort(t, portRaw)
	c := config.Defaults(t.TempDir())
	c.Listeners = c.Listeners[1:]
	c.Security.AllowDirect = true
	c.Security.DenyPrivate = false
	c.Security.DirectAllowlist = []string{host}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "direct"}}}}}}
	runtime, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Durable.Start()
	t.Cleanup(func() {
		runtime.Durable.Stop()
		_ = runtime.Store.Close()
	})
	client, server := net.Pipe()
	served := make(chan struct{})
	go func() {
		runtime.SOCKS.Serve(context.Background(), server)
		close(served)
	}()
	if _, err = client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	request := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
	request = append(request, byte(port>>8), byte(port))
	if _, err = client.Write(request); err != nil {
		t.Fatal(err)
	}
	reply = make([]byte, 10)
	if _, err = io.ReadFull(client, reply); err != nil || reply[1] != 0 {
		t.Fatal(reply, err)
	}
	if _, err = client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2)
	if _, err = io.ReadFull(client, payload); err != nil || string(payload) != "ok" {
		t.Fatal(string(payload), err)
	}
	_ = client.Close()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("SOCKS tunnel did not close")
	}
	<-targetDone
	runtime.Durable.Stop()
	events, err := runtime.Store.ListTraffic(t.Context(), sqlite.TrafficPage{Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	event := events[0]
	if event.Protocol != "socks5" || event.Action != "direct" || event.ClientUpload != 4 || event.ClientDownload != 2 || event.Direct != 6 || event.UpstreamUpload != 0 || event.UpstreamDownload != 0 {
		t.Fatal(event)
	}
}
func parsePort(t *testing.T, raw string) uint16 {
	t.Helper()
	value, err := net.LookupPort("tcp", raw)
	if err != nil || value < 1 || value > 65535 {
		t.Fatal(raw, err)
	}
	return uint16(value)
}
func freeBind(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
