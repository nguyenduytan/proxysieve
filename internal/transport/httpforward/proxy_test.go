package httpforward

import (
	"bufio"
	"context"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func direct(_ context.Context, _ policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
	return policy.Result{Actions: []policy.Action{{Type: "direct"}}}, nil
}
func TestDirectForwardAndCredentialStripping(t *testing.T) {
	var sawProxyAuth string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawProxyAuth = r.Header.Get("Proxy-Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()
	h, err := New(Options{Evaluator: Decider(direct), Resolver: ResolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}), DestinationPolicy: security.DestinationPolicy{AllowTrusted: true}})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(h)
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	req, _ := http.NewRequest(http.MethodGet, target.URL, nil)
	req.Header.Set("Proxy-Authorization", "Basic fake-password")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(b) != "ok" || sawProxyAuth != "" {
		t.Fatalf("%d %s %q", res.StatusCode, b, sawProxyAuth)
	}
}
func TestRejectsUnsafeAndUnsupported(t *testing.T) {
	h, err := New(Options{Evaluator: Decider(direct), Resolver: ResolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}), DestinationPolicy: security.DestinationPolicy{DenyPrivate: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, target, want string }{{http.MethodGet, "http://example.invalid/", "DESTINATION_DENIED"}, {http.MethodConnect, "example.invalid:443", "DESTINATION_DENIED"}} {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		r.RequestURI = ""
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%d %q", w.Code, w.Body.String())
		}
	}
}
func TestConnectDirectTunnel(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "tunnel ok") }))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	h, err := New(Options{Evaluator: Decider(direct), Resolver: ResolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}), DestinationPolicy: security.DestinationPolicy{AllowTrusted: true}})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(h)
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err = io.WriteString(conn, "CONNECT "+targetURL.Host+" HTTP/1.1\r\nHost: "+targetURL.Host+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || res.StatusCode != 200 {
		t.Fatal(res, err)
	}
	if _, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: "+targetURL.Host+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "tunnel ok" {
		t.Fatal(string(body))
	}
}
