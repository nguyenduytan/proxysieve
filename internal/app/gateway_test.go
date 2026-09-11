package app

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
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
