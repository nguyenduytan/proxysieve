package httpforward

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
	"time"

	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	retrypkg "github.com/nguyenduytan/proxysieve/pkg/retry"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type eventRecorder chan trafficpkg.Event

func (r eventRecorder) Record(_ context.Context, event trafficpkg.Event) error {
	r <- event
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func direct(_ context.Context, _ policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
	return policy.Result{PolicyID: "policy", TerminalRuleID: "rule", Actions: []policy.Action{{Type: "direct"}}}, nil
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

func TestCompletesHTTPRouteWithoutTrafficRecorder(t *testing.T) {
	completed := make(chan [2]trafficpkg.Bytes, 1)
	handler, err := New(Options{
		Evaluator: Decider(direct),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "proxy", Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				_, _ = io.ReadAll(request.Body)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}), Complete: func(_ context.Context, upload, download trafficpkg.Bytes) {
				completed <- [2]trafficpkg.Bytes{upload, download}
			}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://example.invalid/", strings.NewReader("payload")))
	if got := <-completed; got != [2]trafficpkg.Bytes{7, 2} {
		t.Fatal(got)
	}
}

func TestAttemptTraceCapturesHealthSignals(t *testing.T) {
	trace := newAttemptTrace(time.Now().Add(-time.Second))
	callbacks := trace.clientTrace()
	callbacks.DNSDone(httptrace.DNSDoneInfo{Err: errors.New("DNS failed")})
	callbacks.ConnectStart("tcp", "proxy:443")
	trace.connectStarted["tcp\x00proxy:443"] = time.Now().Add(-time.Millisecond)
	callbacks.ConnectDone("tcp", "proxy:443", nil)
	callbacks.TLSHandshakeDone(tls.ConnectionState{}, errors.New("TLS failed"))
	callbacks.GotFirstResponseByte()
	observation := trace.observation(false, 0, time.Second, errors.New("request failed"))
	if !observation.DNSFailure || !observation.TLSFailure || observation.ConnectLatency <= 0 || observation.TTFB <= 0 {
		t.Fatal(observation)
	}
}

func TestRecordsPaidBytesBeforeRoundTripFailure(t *testing.T) {
	recorded := make(eventRecorder, 1)
	handler, err := New(Options{
		Evaluator: Decider(func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error) {
			return policy.Result{PolicyID: "policy", TerminalRuleID: "rule", Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}, nil
		}),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "proxy", PoolID: "pool", ProxyID: "proxy", Rate: &trafficpkg.Rate{Price: trafficpkg.Money{Currency: "USD", Micros: 1_000_000_000}, Unit: trafficpkg.GB, EffectiveAt: time.Unix(0, 0)}, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				_, _ = io.ReadAll(request.Body)
				return nil, errors.New("upstream reset")
			})}, nil
		}),
		Recorder: recorded,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.invalid/upload", strings.NewReader("partial-cost"))
	request.RequestURI = ""
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatal(response.Code)
	}
	select {
	case event := <-recorded:
		if event.PolicyID != "policy" || event.RuleID != "rule" || event.StatusCode != http.StatusBadGateway || event.ClientUpload != 12 || event.UpstreamUpload != 12 || event.UpstreamDownload != 0 || event.ClientDownload != 0 || event.ConfiguredCost == nil || event.ConfiguredCost.Amount != (trafficpkg.Money{Currency: "USD", Micros: 12}) {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("failed request traffic was not recorded")
	}
}

func TestHTTPRetryRecordsEachAttempt(t *testing.T) {
	recorded := make(eventRecorder, 2)
	handler, err := New(Options{
		Evaluator: Decider(direct),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{
				Action: "proxy", PoolID: "pool", ProxyID: "first",
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("first failed") }),
				Retry: func(context.Context) (gateway.Route, error) {
					return gateway.Route{Action: "proxy", PoolID: "pool", ProxyID: "second", Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody}, nil
					})}, nil
				},
			}, nil
		}),
		Recorder: recorded,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.invalid/retry", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	first, second := <-recorded, <-recorded
	if first.PolicyID != "policy" || first.RuleID != "rule" || second.PolicyID != "policy" || second.RuleID != "rule" || first.ProxyID != "first" || first.StatusCode != http.StatusBadGateway || second.ProxyID != "second" || second.StatusCode != http.StatusNoContent || first.RequestID != second.RequestID || first.ConnectionID != second.ConnectionID {
		t.Fatal(first, second)
	}
}

func TestHTTPRetryPolicyCanDisableRetry(t *testing.T) {
	retried := false
	retryRules := retrypkg.Policy{MaxAttempts: 1}
	handler, err := New(Options{
		Evaluator: Decider(direct),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{
				Action: "proxy", RetryPolicy: &retryRules,
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("failed") }),
				Retry:     func(context.Context) (gateway.Route, error) { retried = true; return gateway.Route{}, nil },
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.invalid/no-retry", nil))
	if response.Code != http.StatusBadGateway || retried {
		t.Fatal(response.Code, retried)
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
	recorder, _ := internaltraffic.NewMemory(2)
	h, err := New(Options{Evaluator: Decider(direct), Recorder: recorder, Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
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
	events, dropped := recorder.Snapshot()
	if dropped != 0 || len(events) != 2 || events[0].Action != "reject" || events[0].StatusCode != http.StatusForbidden || events[0].Host != "example.invalid" || events[1].Protocol != "connect" || events[1].Action != "reject" || events[1].StatusCode != http.StatusForbidden {
		t.Fatal(events, dropped)
	}
}
func TestDownstreamBearerAuth(t *testing.T) {
	called := false
	recorder, _ := internaltraffic.NewMemory(2)
	h, err := New(Options{Evaluator: Decider(func(_ context.Context, request policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
		called = request.ClientID == "client"
		return direct(context.Background(), request, policy.Visibility{})
	}), Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "reject"}, nil
	}), Recorder: recorder, Authenticate: func(_ context.Context, header string) (model.ID, error) {
		if header != "Bearer fake-key" {
			return "", errors.New("bad")
		}
		return "client", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	req.Header.Set("Proxy-Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusProxyAuthRequired || w.Header().Get("Proxy-Authenticate") != "Bearer" {
		t.Fatal(w.Code, w.Header())
	}
	req = httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	req.Header.Set("Proxy-Authorization", "Bearer fake-key")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called {
		t.Fatal("client identity missing")
	}
	events, _ := recorder.Snapshot()
	if len(events) != 1 || events[0].ClientID != "client" {
		t.Fatal("client identity missing from traffic event", events)
	}
}

func TestDownstreamBasicPasswordAuth(t *testing.T) {
	called := false
	h, err := New(Options{
		Evaluator: Decider(func(_ context.Context, request policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
			_, leaked := request.Headers.Value["Proxy-Authorization"]
			called = request.ClientID == "client-alice" && !leaked
			return policy.Result{Actions: []policy.Action{{Type: "reject"}}}, nil
		}),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "reject"}, nil
		}),
		AuthenticatePassword: func(_ context.Context, username, password string) (model.ID, error) {
			if username != "alice" || password != "correct horse" {
				return "", errors.New("bad credentials")
			}
			return "client-alice", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	request.Header.Set("Proxy-Authorization", "Basic YWxpY2U6Y29ycmVjdCBob3JzZQ==")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !called {
		t.Fatalf("status=%d called=%v body=%q", response.Code, called, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	request.Header.Set("Proxy-Authorization", "Basic YWxpY2U6d3Jvbmc=")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusProxyAuthRequired || response.Header().Get("Proxy-Authenticate") == "" {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
}
func TestConnectDirectTunnel(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "tunnel ok") }))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	recorded := make(eventRecorder, 1)
	h, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect), Recorder: recorded})
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
	requestBytes := "GET / HTTP/1.1\r\nHost: " + targetURL.Host + "\r\nConnection: close\r\n\r\n"
	if _, err = io.WriteString(conn, requestBytes); err != nil {
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
	_ = conn.Close()
	select {
	case event := <-recorded:
		if event.PolicyID != "policy" || event.RuleID != "rule" || event.Protocol != "connect" || event.Action != "direct" || event.StatusCode != http.StatusOK || event.ClientUpload != trafficpkg.Bytes(len(requestBytes)) || event.ClientDownload == 0 || event.Direct != event.ClientUpload+event.ClientDownload || event.UpstreamUpload != 0 || event.UpstreamDownload != 0 {
			t.Fatal(event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CONNECT traffic event was not recorded")
	}
}
