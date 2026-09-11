package httpforward

import (
	"bufio"
	"context"
	"errors"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func direct(_ context.Context, _ policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
	return policy.Result{Actions: []policy.Action{{Type: "direct"}}}, nil
}
func routeDirect(_ context.Context, _ policy.RequestContext, _ policy.Result) (gateway.Route, error) {
	return gateway.Route{Action: "direct", Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}, Dial: func(ctx context.Context, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}}, nil
}
func TestDirectForwardAndCredentialStripping(t *testing.T) {
	var sawProxyAuth string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawProxyAuth = r.Header.Get("Proxy-Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()
	h, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect)})
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
func TestRecordsActualHTTPStreamBytes(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte("reply:"), body...))
	}))
	defer target.Close()
	recorder, _ := internaltraffic.NewMemory(1)
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect), Recorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	response, err := client.Post(target.URL, "text/plain", strings.NewReader("input"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "reply:input" {
		t.Fatal(string(body))
	}
	events, dropped := recorder.Snapshot()
	if dropped != 0 || len(events) != 1 {
		t.Fatal(events, dropped)
	}
	event := events[0]
	if event.ClientUpload != 5 || event.UpstreamUpload != 0 || event.ClientDownload != 11 || event.UpstreamDownload != 0 || event.Direct != 16 {
		t.Fatal(event)
	}
}
func TestSafeResponseCache(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("cacheable"))
	}))
	defer target.Close()
	cache, _ := internalcache.NewMemory(10, 1024)
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect), ResponseCache: cache, MaxCacheBody: 1024})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	for range 2 {
		response, err := client.Get(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if string(body) != "cacheable" {
			t.Fatal(string(body))
		}
	}
	if hits != 1 {
		t.Fatal("cache missed", hits)
	}
}
func TestCacheRejectsCookieResponses(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Set-Cookie", "private=1")
		_, _ = w.Write([]byte("private"))
	}))
	defer target.Close()
	cache, _ := internalcache.NewMemory(10, 1024)
	handler, _ := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect), ResponseCache: cache, MaxCacheBody: 1024})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	for range 2 {
		response, err := client.Get(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if hits != 2 {
		t.Fatal("private response cached", hits)
	}
}
func TestRejectsUnsafeAndUnsupported(t *testing.T) {
	h, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{}, errors.New("denied")
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, target, want string }{{http.MethodGet, "http://example.invalid/", "POLICY_BLOCKED"}, {http.MethodConnect, "example.invalid:443", "DESTINATION_DENIED"}} {
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
	h, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect)})
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
