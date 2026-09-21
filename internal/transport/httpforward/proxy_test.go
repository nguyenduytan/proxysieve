package httpforward

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	internalinspect "github.com/nguyenduytan/proxysieve/internal/inspect"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
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

type hijackResponse struct {
	header http.Header
	conn   net.Conn
}

func (w *hijackResponse) Header() http.Header       { return w.header }
func (w *hijackResponse) Write([]byte) (int, error) { return 0, nil }
func (w *hijackResponse) WriteHeader(int)           {}
func (w *hijackResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func direct(_ context.Context, _ policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
	return policy.Result{PolicyID: "policy", TerminalRuleID: "rule", Actions: []policy.Action{{Type: "direct"}}}, nil
}
func routeDirect(_ context.Context, _ policy.RequestContext, _ policy.Result) (gateway.Route, error) {
	return gateway.Route{Action: "direct", Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}, Dial: func(ctx context.Context, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}}, nil
}

func routeCachedDirect(ctx context.Context, request policy.RequestContext, result policy.Result) (gateway.Route, error) {
	route, err := routeDirect(ctx, request, result)
	route.Cache = true
	return route, err
}

func TestInspectCONNECTUsesVisibleHTTPSPipeline(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := internalinspect.Create(dataDir, false); err != nil {
		t.Fatal(err)
	}
	inspector, err := internalinspect.Open(dataDir, []string{"api.example.invalid"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	visible := make(chan policy.RequestContext, 1)
	handler, err := New(Options{
		Evaluator: Decider(func(_ context.Context, request policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
			if request.Protocol == "http" {
				visible <- request
			}
			return policy.Result{PolicyID: "policy", TerminalRuleID: "rule", Actions: []policy.Action{{Type: "direct"}}}, nil
		}),
		Router: RouterFunc(func(_ context.Context, request policy.RequestContext, _ policy.Result) (gateway.Route, error) {
			if request.Protocol == "connect" {
				return gateway.Route{Action: "direct", Dial: func(context.Context, string) (net.Conn, error) {
					return nil, errors.New("inspect must not open a tunnel")
				}}, nil
			}
			return gateway.Route{Action: "direct", Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("inspected:" + request.URL.RequestURI()))}, nil
			})}, nil
		}),
		Inspector: inspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(proxyServer.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err = io.WriteString(connection, "CONNECT api.example.invalid:443 HTTP/1.1\r\nHost: api.example.invalid:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	connectResponse, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || connectResponse.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT response=%v error=%v", connectResponse, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(inspector.CertificatePEM()) {
		t.Fatal("inspect root was not parsed")
	}
	tlsConnection := tls.Client(&bufferedConn{Conn: connection, reader: reader}, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "api.example.invalid", NextProtos: []string{"http/1.1"}})
	if err = tlsConnection.Handshake(); err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(tlsConnection, "GET /private?item=1 HTTP/1.1\r\nHost: api.example.invalid\r\nX-Test: visible\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(tlsConnection), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "inspected:/private?item=1" {
		t.Fatalf("response=%d body=%q", response.StatusCode, body)
	}
	select {
	case request := <-visible:
		if request.Scheme != "https" || request.Host != "api.example.invalid" || !request.Path.Known || request.Path.Value != "/private" || http.Header(request.Headers.Value).Get("X-Test") != "visible" {
			t.Fatalf("visible request mismatch: %+v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("inspected request did not reach policy evaluation")
	}
}

func TestInspectAuthority(t *testing.T) {
	for _, test := range []struct {
		value string
		host  string
		port  uint16
		valid bool
	}{
		{"example.invalid", "example.invalid", 443, true},
		{"example.invalid:8443", "example.invalid", 8443, true},
		{"[::1]:443", "::1", 443, true},
		{"other.invalid:", "", 0, false},
		{"bad host", "", 0, false},
	} {
		host, port, valid := inspectAuthority(test.value)
		if host != test.host || port != test.port || valid != test.valid {
			t.Fatalf("%q = %q %d %v", test.value, host, port, valid)
		}
	}
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

func TestConcurrentHTTPForwardLoad(t *testing.T) {
	const workers, requestsPerWorker = 16, 32
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()

	upstream := &http.Transport{ForceAttemptHTTP2: false, MaxIdleConns: workers, MaxIdleConnsPerHost: workers}
	defer upstream.CloseIdleConnections()
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "direct", Transport: upstream}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()
	proxyURL, _ := url.Parse(proxyServer.URL)
	clientTransport := &http.Transport{Proxy: http.ProxyURL(proxyURL), MaxIdleConns: workers, MaxIdleConnsPerHost: workers}
	defer clientTransport.CloseIdleConnections()
	client := &http.Client{Transport: clientTransport, Timeout: 10 * time.Second}

	failures := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range requestsPerWorker {
				response, requestErr := client.Get(target.URL)
				if requestErr == nil {
					_, requestErr = io.Copy(io.Discard, response.Body)
					requestErr = errors.Join(requestErr, response.Body.Close())
					if response.StatusCode != http.StatusOK {
						requestErr = errors.Join(requestErr, fmt.Errorf("status %d", response.StatusCode))
					}
				}
				if requestErr != nil {
					failures <- requestErr
					return
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for requestErr := range failures {
		t.Fatal(requestErr)
	}
	if got, want := hits.Load(), int64(workers*requestsPerWorker); got != want {
		t.Fatalf("origin hits = %d, want %d", got, want)
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
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("cacheable"))
	}))
	defer target.Close()
	cache, _ := internalcache.NewMemory(10, 1024)
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeCachedDirect), ResponseCache: cache, MaxCacheBody: 1024})
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
	stats := cache.Stats(time.Now().UTC())
	if stats.Hits != 1 || stats.Misses != 1 || stats.BytesServed != 9 {
		t.Fatalf("unexpected cache stats: %+v", stats)
	}
}

func TestResponseCacheRequiresPolicyAction(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(w, "cacheable")
	}))
	defer target.Close()
	cache, _ := internalcache.NewMemory(10, 1024)
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect), ResponseCache: cache, MaxCacheBody: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target.URL, nil))
		if response.Code != http.StatusOK || response.Body.String() != "cacheable" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if stats := cache.Stats(time.Now().UTC()); hits != 2 || stats.Entries != 0 || stats.Hits != 0 || stats.Misses != 0 {
		t.Fatal(hits, stats)
	}
}
func TestDiskResponseCacheSurvivesHandlerRestart(t *testing.T) {
	var upstreamHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits++
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(w, "persisted")
	}))
	defer target.Close()
	dir := t.TempDir()
	for range 2 {
		responseCache, err := internalcache.NewDisk(dir, 10, 1024)
		if err != nil {
			t.Fatal(err)
		}
		handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeCachedDirect), ResponseCache: responseCache, MaxCacheBody: 1024})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target.URL, nil))
		if response.Code != http.StatusOK || response.Body.String() != "persisted" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if upstreamHits != 1 {
		t.Fatal("disk cache did not survive handler restart", upstreamHits)
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
	handler, _ := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeCachedDirect), ResponseCache: cache, MaxCacheBody: 1024})
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

func TestOversizedCacheCandidateKeepsBudgetEnforcement(t *testing.T) {
	manager, err := internalbudget.New([]publicbudget.Config{{ID: "system", Name: "System", Limit: 7, Hard: true, Action: publicbudget.ActionReject}}, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	cache, _ := internalcache.NewMemory(10, 1024)
	recorded := make(eventRecorder, 1)
	handler, err := New(Options{
		Evaluator: Decider(direct),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "proxy", Cache: true, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Header: http.Header{"Cache-Control": {"max-age=60"}}, Body: io.NopCloser(strings.NewReader("0123456789"))}, nil
			}), Reserve: func(ctx context.Context, amount trafficpkg.Bytes) (publicbudget.Lease, error) {
				return manager.Reserve(ctx, []model.ID{"system"}, amount)
			}}, nil
		}),
		Recorder:      recorded,
		ResponseCache: cache,
		MaxCacheBody:  4,
	})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.invalid/large", nil))
	if response.Body.String() != "0123456" {
		t.Fatalf("budgeted body = %q", response.Body.String())
	}
	event := <-recorded
	if event.ClientDownload != 7 || event.UpstreamDownload != 7 {
		t.Fatal(event)
	}
	usage, err := manager.Usage("system")
	if err != nil || usage.Used != 7 || usage.Reserved != 0 {
		t.Fatal(usage, err)
	}
	if stats := cache.Stats(time.Now().UTC()); stats.Entries != 0 {
		t.Fatal("oversized response was cached", stats)
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
func TestReportsUnavailableActions(t *testing.T) {
	recorded := make(eventRecorder, 2)
	h, err := New(Options{Evaluator: Decider(direct), Recorder: recorded, Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "cache"}, gateway.ErrUnsupported
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, target string }{{http.MethodGet, "http://example.invalid/"}, {http.MethodConnect, "example.invalid:443"}} {
		request := httptest.NewRequest(tc.method, tc.target, nil)
		request.RequestURI = ""
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "ACTION_UNAVAILABLE") {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	}
	for range 2 {
		if event := <-recorded; event.Action != "cache" || event.StatusCode != http.StatusNotImplemented {
			t.Fatal(event)
		}
	}
}

func TestVisibleHTTPResponseActions(t *testing.T) {
	for _, tc := range []struct {
		name, action, value, location, body string
		status                              int
	}{
		{name: "mock", action: "mock", value: "synthetic", status: http.StatusOK, body: "synthetic"},
		{name: "redirect", action: "redirect", value: "/login", status: http.StatusFound, location: "/login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorded := make(eventRecorder, 1)
			handler, err := New(Options{Evaluator: Decider(direct), Recorder: recorded, Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
				return gateway.Route{Action: tc.action, ActionValue: tc.value}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.invalid/original", nil))
			if response.Code != tc.status || response.Body.String() != tc.body || response.Header().Get("Location") != tc.location {
				t.Fatalf("status=%d body=%q headers=%v", response.Code, response.Body.String(), response.Header())
			}
			if event := <-recorded; event.Action != tc.action || event.StatusCode != tc.status || event.ClientDownload != trafficpkg.Bytes(len(tc.body)) {
				t.Fatal(event)
			}
		})
	}
}

func TestRewriteStaysOnOriginalDestination(t *testing.T) {
	var target string
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "direct", RewritePath: "/v2/items?limit=2", Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			target = request.URL.String()
			return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody}, nil
		})}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.invalid/v1/items", nil))
	if response.Code != http.StatusNoContent || target != "http://example.invalid/v2/items?limit=2" {
		t.Fatal(response.Code, target)
	}
}

func TestExplicitCacheFailsClosedWhenDisabled(t *testing.T) {
	called := false
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "direct", Cache: true, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected")
		})}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil))
	if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "CACHE_UNAVAILABLE") || called {
		t.Fatal(response.Code, response.Body.String(), called)
	}
}

func TestThrottleAppliesToCacheHits(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(w, "1234")
	}))
	defer target.Close()
	cache, _ := internalcache.NewMemory(10, 1024)
	handler, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(func(ctx context.Context, request policy.RequestContext, result policy.Result) (gateway.Route, error) {
		route, routeErr := routeCachedDirect(ctx, request, result)
		route.ThrottleBPS = 80
		return route, routeErr
	}), ResponseCache: cache, MaxCacheBody: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		started := time.Now()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target.URL, nil))
		if response.Code != http.StatusOK || response.Body.String() != "1234" || time.Since(started) < 40*time.Millisecond {
			t.Fatal(attempt, response.Code, response.Body.String(), time.Since(started))
		}
	}
	if stats := cache.Stats(time.Now().UTC()); stats.Hits != 1 {
		t.Fatal(stats)
	}
}
func TestDownstreamBearerAuth(t *testing.T) {
	called := false
	recorder, _ := internaltraffic.NewMemory(2)
	h, err := New(Options{Evaluator: Decider(func(_ context.Context, request policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
		called = request.ClientID == "client" && request.Listener == "edge-http"
		return direct(context.Background(), request, policy.Visibility{})
	}), Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
		return gateway.Route{Action: "reject"}, nil
	}), Recorder: recorder, Authenticate: func(_ context.Context, header string) (model.ID, error) {
		if header != "Bearer fake-key" {
			return "", errors.New("bad")
		}
		return "client", nil
	}, ListenerName: "edge-http"})
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
	h, err := New(Options{Evaluator: Decider(direct), Router: RouterFunc(func(ctx context.Context, request policy.RequestContext, result policy.Result) (gateway.Route, error) {
		route, routeErr := routeDirect(ctx, request, result)
		route.ThrottleBPS = 1000
		return route, routeErr
	}), Recorder: recorded})
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
	started := time.Now()
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
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatal("CONNECT throttle completed too quickly", elapsed)
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

func TestConnectCancellationClosesTunnel(t *testing.T) {
	client, server := net.Pipe()
	upstream, target := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = target.Close() }()
	h, err := New(Options{
		Evaluator: Decider(direct),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "direct", Dial: func(context.Context, string) (net.Conn, error) { return upstream, nil }}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodConnect, "http://example.invalid", nil).WithContext(ctx)
	request.Host = "example.invalid:443"
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(&hijackResponse{header: http.Header{}, conn: server}, request)
		close(done)
	}()
	response, err := http.ReadResponse(bufio.NewReader(client), request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal(response, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled CONNECT tunnel did not close")
	}
}

func TestConnectLargeStreamAccounting(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	go func() {
		connection, acceptErr := target.Accept()
		if acceptErr == nil {
			defer func() { _ = connection.Close() }()
			_, _ = io.Copy(connection, connection)
		}
	}()
	recorded := make(eventRecorder, 1)
	h, err := New(Options{
		Evaluator: Decider(direct),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "direct", Dial: func(ctx context.Context, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", address)
			}}, nil
		}),
		Recorder: recorded,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(h)
	defer proxy.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintf(connection, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target.Addr(), target.Addr()); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal(response, err)
	}
	payload := bytes.Repeat([]byte("proxysieve"), 100_000)
	writeErr := make(chan error, 1)
	go func() {
		_, err := connection.Write(payload)
		writeErr <- err
	}()
	received := make([]byte, len(payload))
	if _, err = io.ReadFull(reader, received); err != nil || !bytes.Equal(received, payload) {
		t.Fatal("large CONNECT payload mismatch", err)
	}
	if err = <-writeErr; err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	select {
	case event := <-recorded:
		if event.ClientUpload != trafficpkg.Bytes(len(payload)) || event.ClientDownload != trafficpkg.Bytes(len(payload)) || event.Direct != trafficpkg.Bytes(2*len(payload)) {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("large CONNECT stream was not recorded")
	}
}
