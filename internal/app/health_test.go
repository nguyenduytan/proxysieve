package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	internalhealth "github.com/nguyenduytan/proxysieve/internal/health"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestHealthControlReportsRuntimeEligibility(t *testing.T) {
	base := config.Defaults(t.TempDir())
	endpoints := []proxy.Endpoint{
		{ID: "healthy", Name: "Healthy", Protocol: proxy.HTTP, Host: "healthy.example.invalid", Port: 8080, Enabled: true},
		{ID: "degraded", Name: "Degraded", Protocol: proxy.HTTP, Host: "degraded.example.invalid", Port: 8080, Enabled: true},
		{ID: "disabled", Name: "Disabled", Protocol: proxy.HTTP, Host: "disabled.example.invalid", Port: 8080},
	}
	pool := routing.Pool{ID: "pool", Name: "Pool", Strategy: routing.HighestHealth, EndpointIDs: []model.ID{"healthy", "degraded", "disabled"}, MinHealthScore: 60, Enabled: true}
	disabledPool := pool
	disabledPool.ID = "disabled-pool"
	disabledPool.Enabled = false
	bundle := store.RuntimeBundle{Proxies: endpoints, Pools: []routing.Pool{pool, disabledPool}, Policies: base.Policies}
	runtime, err := newRoutingRuntime(base, bundle, store.RuntimeRecord{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := internalhealth.New(publichealth.Defaults(), nil)
	for range 4 {
		_, _ = manager.Observe("healthy", publichealth.Observation{Success: true, Latency: 10 * time.Millisecond})
	}
	_, _ = manager.Observe("degraded", publichealth.Observation{Success: false})
	control := &healthControl{runtime: runtime, health: manager}
	proxies, err := control.ProxyHealth(t.Context())
	if err != nil || len(proxies) != 3 || proxies[0].State != publichealth.Healthy || proxies[1].State != publichealth.Degraded || proxies[2].State != publichealth.Disabled {
		t.Fatal(proxies, err)
	}
	pools, err := control.PoolHealth(t.Context())
	if err != nil || len(pools) != 2 || pools[0].Eligible != 1 || pools[0].Healthy != 1 || pools[0].Degraded != 1 || pools[0].Disabled != 1 || pools[1].Eligible != 0 {
		t.Fatal(pools, err)
	}
}

func TestManualHealthCheckRecordsHandshakeTraffic(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	targetHost, targetPort, _ := net.SplitHostPort(targetURL.Host)
	proxyServer := newConnectProxy(t, make(chan string, 1))
	defer proxyServer.Close()
	proxyURL, _ := url.Parse(proxyServer.URL)
	proxyHost, proxyPort, _ := net.SplitHostPort(proxyURL.Host)
	base := config.Defaults(t.TempDir())
	endpoint := proxy.Endpoint{ID: "proxy", Name: "Proxy", Protocol: proxy.HTTP, Host: proxyHost, Port: parsePort(t, proxyPort), Enabled: true, TrustedRemoteDNS: true}
	bundle := store.RuntimeBundle{Proxies: []proxy.Endpoint{endpoint}, Policies: base.Policies}
	runtime, err := newRoutingRuntime(base, bundle, store.RuntimeRecord{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := internalhealth.New(publichealth.Defaults(), nil)
	recorder, _ := internaltraffic.NewMemory(1)
	control := &healthControl{runtime: runtime, health: manager, resolver: fixedResolver{addresses: []netip.Addr{netip.MustParseAddr(targetHost)}}, destination: security.DestinationPolicy{AllowTrusted: true}, recorder: recorder, timeout: time.Second}
	result, err := control.CheckProxy(t.Context(), "proxy", targetHost, parsePort(t, targetPort))
	if err != nil || result.State != publichealth.Healthy {
		t.Fatal(result, err)
	}
	events, dropped := recorder.Snapshot()
	if dropped != 0 || len(events) != 1 || events[0].Action != "health_check" || events[0].ProxyID != "proxy" || events[0].HealthCheck == 0 {
		t.Fatal(events, dropped)
	}
}

func TestHealthCheckSerializesEachEndpoint(t *testing.T) {
	manager, _ := internalhealth.New(publichealth.Defaults(), nil)
	control := &healthControl{health: manager}
	endpoint := proxy.Endpoint{ID: "proxy", Enabled: true}
	control.checks.Store(endpoint.ID, struct{}{})
	if _, attempted := control.checkEndpoint(t.Context(), endpoint, "", "example.com", 443); attempted {
		t.Fatal("overlapping health check was admitted")
	}
	control.checks.Delete(endpoint.ID)
}
