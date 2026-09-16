// Package httpforward implements the visible HTTP forward-proxy adapter.
package httpforward

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"time"

	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	cachepkg "github.com/nguyenduytan/proxysieve/pkg/cache"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	retrypkg "github.com/nguyenduytan/proxysieve/pkg/retry"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

const sessionHeader = "X-ProxySieve-Session"

type Decider func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)

func (f Decider) Evaluate(ctx context.Context, r policy.RequestContext, v policy.Visibility) (policy.Result, error) {
	return f(ctx, r, v)
}

type RouterFunc func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error)

func (f RouterFunc) Route(ctx context.Context, r policy.RequestContext, result policy.Result) (gateway.Route, error) {
	return f(ctx, r, result)
}

type Options struct {
	Evaluator            gateway.Evaluator
	Router               gateway.Router
	Recorder             trafficpkg.Recorder
	Authenticate         func(context.Context, string) (model.ID, error)
	AuthenticatePassword func(context.Context, string, string) (model.ID, error)
	ResponseCache        *internalcache.Memory
	MaxCacheBody         int64
	MaxHeaderBytes       int
}
type Handler struct {
	evaluator            gateway.Evaluator
	router               gateway.Router
	recorder             trafficpkg.Recorder
	authenticate         func(context.Context, string) (model.ID, error)
	authenticatePassword func(context.Context, string, string) (model.ID, error)
	responseCache        *internalcache.Memory
	maxCacheBody         int64
	maxHeaderBytes       int
}

func New(options Options) (*Handler, error) {
	if options.Evaluator == nil || options.Router == nil {
		return nil, errors.New("http forward proxy dependencies are required")
	}
	if options.MaxHeaderBytes == 0 {
		options.MaxHeaderBytes = 32 << 10
	}
	if options.MaxHeaderBytes < 1024 || options.MaxHeaderBytes > 1<<20 {
		return nil, errors.New("invalid header limit")
	}
	if options.MaxCacheBody == 0 {
		options.MaxCacheBody = 1 << 20
	}
	if options.MaxCacheBody < 1 || options.MaxCacheBody > 8<<20 {
		return nil, errors.New("invalid cache response limit")
	}
	return &Handler{evaluator: options.Evaluator, router: options.Router, recorder: options.Recorder, authenticate: options.Authenticate, authenticatePassword: options.AuthenticatePassword, responseCache: options.ResponseCache, maxCacheBody: options.MaxCacheBody, maxHeaderBytes: options.MaxHeaderBytes}, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientID, ok := h.authenticateRequest(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodConnect {
		h.connect(w, r, clientID)
		return
	}
	if len(r.Header.Get(sessionHeader)) > 4096 {
		http.Error(w, "INVALID_SESSION_KEY", http.StatusBadRequest)
		return
	}
	if r.URL == nil || !r.URL.IsAbs() || r.URL.Host == "" || r.URL.User != nil || r.Host == "" || defaultPort(r.URL.Scheme) == 0 {
		http.Error(w, "INVALID_PROXY_REQUEST", http.StatusBadRequest)
		return
	}
	if r.ContentLength > 1<<30 {
		http.Error(w, "REQUEST_TOO_LARGE", http.StatusRequestEntityTooLarge)
		return
	}
	host := r.URL.Hostname()
	port := uint16(defaultPort(r.URL.Scheme))
	if raw := r.URL.Port(); raw != "" {
		p, err := strconv.ParseUint(raw, 10, 16)
		if err != nil || p == 0 {
			http.Error(w, "INVALID_DESTINATION", 400)
			return
		}
		port = uint16(p)
	}
	if !proxy.ValidHost(host) || port == 0 || strings.HasSuffix(r.URL.Host, ":") {
		http.Error(w, "INVALID_DESTINATION", http.StatusBadRequest)
		return
	}
	visibleHeaders := cloneHeader(r.Header)
	for key := range visibleHeaders {
		lower := strings.ToLower(key)
		if strings.EqualFold(key, "Proxy-Authorization") || strings.HasPrefix(lower, "x-proxysieve-") {
			delete(visibleHeaders, key)
		}
	}
	ctx := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), ClientID: clientID, Listener: "http", Protocol: "http", Scheme: r.URL.Scheme, Host: host, SessionKey: r.Header.Get(sessionHeader), Port: port, Method: model.Optional[string]{Known: true, Value: r.Method}, Path: model.Optional[string]{Known: true, Value: r.URL.EscapedPath()}, Headers: model.Optional[map[string][]string]{Known: true, Value: visibleHeaders}, Timestamp: time.Now().UTC()}
	result, err := h.evaluator.Evaluate(r.Context(), ctx, policy.Visibility{Host: true, Method: true, Path: true, Headers: true})
	if err != nil {
		recordHTTP(h.recorder, r, ctx, gateway.Route{Action: "reject", PolicyID: result.PolicyID, RuleID: result.TerminalRuleID}, host, 0, 0, 0, http.StatusForbidden, 0)
		http.Error(w, "POLICY_REJECTED", http.StatusForbidden)
		return
	}
	route, err := h.router.Route(r.Context(), ctx, result)
	route.PolicyID, route.RuleID = result.PolicyID, result.TerminalRuleID
	if err != nil || route.Action == "block" || route.Action == "reject" || route.Action == "" {
		if route.Action != "block" && route.Action != "reject" {
			route.Action = "reject"
		}
		recordHTTP(h.recorder, r, ctx, route, host, 0, 0, 0, http.StatusForbidden, 0)
		if errors.Is(err, gateway.ErrDenied) {
			http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
			return
		}
		http.Error(w, "POLICY_BLOCKED", http.StatusForbidden)
		return
	}
	if route.Transport == nil {
		recordHTTP(h.recorder, r, ctx, route, host, 0, 0, 0, http.StatusBadGateway, 0)
		http.Error(w, "ROUTE_UNAVAILABLE", http.StatusBadGateway)
		return
	}
	if err = internalbudget.Available(r.Context(), route.Reserve); err != nil {
		route.Action = "budget_reject"
		recordHTTP(h.recorder, r, ctx, route, host, 0, 0, 0, http.StatusTooManyRequests, 0)
		http.Error(w, "BUDGET_EXCEEDED", http.StatusTooManyRequests)
		return
	}
	cacheKey := cacheKeyFor(route, ctx, r)
	cacheEligible := cachepkg.CheckRequest(r).Eligible
	if h.responseCache != nil && !cacheEligible {
		h.responseCache.RecordBypass()
	}
	if h.responseCache != nil && cacheEligible {
		if cached, ok := h.responseCache.Get(cacheKey, time.Now().UTC()); ok {
			copyHeader(w.Header(), http.Header(cached.Header))
			w.WriteHeader(cached.Status)
			delivered := &internaltraffic.Writer{Destination: w}
			_, copyErr := delivered.Write(cached.Body)
			completeRoute(r.Context(), route, 0, 0)
			if h.recorder != nil {
				_ = h.recorder.Record(context.WithoutCancel(r.Context()), trafficpkg.Event{At: time.Now().UTC(), RequestID: ctx.RequestID, ConnectionID: ctx.ConnectionID, ClientID: ctx.ClientID, PolicyID: route.PolicyID, RuleID: route.RuleID, PoolID: route.PoolID, ProxyID: route.ProxyID, ChainID: route.ChainID, Host: host, Protocol: "http", Action: "cache", StatusCode: cached.Status, ClientDownload: delivered.Bytes(), CacheServed: delivered.Bytes()})
			}
			if copyErr != nil {
				panic(http.ErrAbortHandler)
			}
			return
		}
	}
	if route.Acquire != nil && !route.Acquire() {
		recordHTTP(h.recorder, r, ctx, route, host, 0, 0, 0, http.StatusBadGateway, 0)
		http.Error(w, "ROUTE_UNAVAILABLE", http.StatusBadGateway)
		return
	}
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	upload := &internaltraffic.Reader{Source: body}
	out := r.Clone(r.Context())
	out.Body = &countedBody{Reader: &internalbudget.Reader{Context: r.Context(), Source: upload, Reserve: route.Reserve}, Closer: body}
	if body == http.NoBody {
		out.Body = http.NoBody
	}
	out.RequestURI = ""
	out.Host = r.URL.Host
	out.Header = cloneHeader(r.Header)
	stripHopByHop(out.Header)
	out.Header.Del("Proxy-Authorization")
	out.Header.Del("Proxy-Connection")
	var response *http.Response
	attempt := uint8(1)
	retryPolicy := retrypkg.DefaultPolicy()
	if route.RetryPolicy != nil {
		retryPolicy = *route.RetryPolicy
	}
	for {
		started := time.Now()
		trace := newAttemptTrace(started)
		attemptRequest := out.Clone(httptrace.WithClientTrace(out.Context(), trace.clientTrace()))
		response, err = route.Transport.RoundTrip(attemptRequest)
		latency := time.Since(started)
		if err == nil {
			if route.Observe != nil {
				route.Observe(trace.observation(true, response.StatusCode, latency, nil))
			}
			break
		}
		if route.Observe != nil {
			route.Observe(trace.observation(false, 0, latency, errors.Join(err, r.Context().Err())))
		}
		recordHTTP(h.recorder, r, ctx, route, host, upload.Bytes(), 0, 0, http.StatusBadGateway, 0)
		canRetry := route.Retry != nil && retryPolicy.ShouldRetry(retrypkg.Request{Method: r.Method, BodyPresent: r.ContentLength != 0 || len(r.TransferEncoding) > 0, IdempotencyKey: r.Header.Get("Idempotency-Key") != "", Attempt: attempt, Failure: retrypkg.ConnectFailure})
		if !canRetry || retrypkg.Wait(r.Context(), attempt) != nil {
			break
		}
		next, retryErr := route.Retry(r.Context())
		if retryErr != nil || next.Transport == nil || internalbudget.Available(r.Context(), next.Reserve) != nil || next.Acquire != nil && !next.Acquire() {
			break
		}
		next.PolicyID, next.RuleID = result.PolicyID, result.TerminalRuleID
		route = next
		attempt++
	}
	if err != nil {
		http.Error(w, "UPSTREAM_FAILED", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	cacheKey = cacheKeyFor(route, ctx, r)
	download := &internaltraffic.Reader{Source: &internalbudget.Reader{Context: r.Context(), Source: response.Body, Reserve: route.Reserve}}
	responseHeaders := response.Header.Clone()
	stripHopByHop(responseHeaders)
	cacheEligibility := cachepkg.CheckResponse(response.StatusCode, response.Header, response.ContentLength, h.maxCacheBody, time.Now().UTC())
	if h.responseCache != nil && cachepkg.CheckRequest(r).Eligible && cacheEligibility.Eligible {
		body, readErr := io.ReadAll(io.LimitReader(download, h.maxCacheBody+1))
		if readErr == nil && int64(len(body)) <= h.maxCacheBody {
			_ = h.responseCache.Put(cacheKey, internalcache.Entry{Status: response.StatusCode, Header: responseHeaders, Body: body, ExpiresAt: cacheEligibility.ExpiresAt})
			copyHeader(w.Header(), responseHeaders)
			w.WriteHeader(response.StatusCode)
			delivered := &internaltraffic.Writer{Destination: w}
			_, copyErr := delivered.Write(body)
			recordHTTP(h.recorder, r, ctx, route, host, upload.Bytes(), download.Bytes(), delivered.Bytes(), response.StatusCode, 0)
			if copyErr != nil {
				panic(http.ErrAbortHandler)
			}
			return
		}
		download = &internaltraffic.Reader{Source: io.MultiReader(bytes.NewReader(body), response.Body)}
	}
	copyHeader(w.Header(), responseHeaders)
	w.WriteHeader(response.StatusCode)
	delivered := &internaltraffic.Writer{Destination: w}
	_, copyErr := io.Copy(delivered, download)
	recordHTTP(h.recorder, r, ctx, route, host, upload.Bytes(), download.Bytes(), delivered.Bytes(), response.StatusCode, 0)
	if copyErr != nil {
		panic(http.ErrAbortHandler)
	}
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request, clientID model.ID) {
	if len(r.Header.Get(sessionHeader)) > 4096 {
		http.Error(w, "INVALID_SESSION_KEY", http.StatusBadRequest)
		return
	}
	host, portRaw, err := net.SplitHostPort(r.Host)
	if err != nil || !proxy.ValidHost(host) {
		http.Error(w, "INVALID_CONNECT_TARGET", http.StatusBadRequest)
		return
	}
	port64, err := strconv.ParseUint(portRaw, 10, 16)
	if err != nil || port64 == 0 {
		http.Error(w, "INVALID_CONNECT_TARGET", http.StatusBadRequest)
		return
	}
	ctx := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), ClientID: clientID, Listener: "http", Protocol: "connect", Host: host, SessionKey: r.Header.Get(sessionHeader), Port: uint16(port64), Method: model.Optional[string]{Known: true, Value: http.MethodConnect}, Timestamp: time.Now().UTC()}
	result, err := h.evaluator.Evaluate(r.Context(), ctx, policy.Visibility{Host: true, Method: true})
	if err != nil {
		recordTunnel(h.recorder, r.Context(), ctx, gateway.Route{Action: "reject", PolicyID: result.PolicyID, RuleID: result.TerminalRuleID}, host, "connect", http.StatusForbidden, 0, 0, 0, 0)
		http.Error(w, "POLICY_BLOCKED", http.StatusForbidden)
		return
	}
	route, err := h.router.Route(r.Context(), ctx, result)
	route.PolicyID, route.RuleID = result.PolicyID, result.TerminalRuleID
	if err != nil || route.Dial == nil {
		if route.Action != "block" && route.Action != "reject" {
			route.Action = "reject"
		}
		recordTunnel(h.recorder, r.Context(), ctx, route, host, "connect", http.StatusForbidden, 0, 0, 0, 0)
		if errors.Is(err, gateway.ErrDenied) {
			http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
			return
		}
		http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
		return
	}
	if err = internalbudget.Available(r.Context(), route.Reserve); err != nil {
		route.Action = "budget_reject"
		recordTunnel(h.recorder, r.Context(), ctx, route, host, "connect", http.StatusTooManyRequests, 0, 0, 0, 0)
		http.Error(w, "BUDGET_EXCEEDED", http.StatusTooManyRequests)
		return
	}
	if route.Acquire != nil && !route.Acquire() {
		recordTunnel(h.recorder, r.Context(), ctx, route, host, "connect", http.StatusBadGateway, 0, 0, 0, 0)
		http.Error(w, "ROUTE_UNAVAILABLE", http.StatusBadGateway)
		return
	}
	var upstream net.Conn
	attempt := uint8(1)
	retryPolicy := retrypkg.DefaultPolicy()
	if route.RetryPolicy != nil {
		retryPolicy = *route.RetryPolicy
	}
	for {
		started := time.Now()
		upstream, err = route.Dial(r.Context(), net.JoinHostPort(host, portRaw))
		latency := time.Since(started)
		if err == nil {
			if route.Observe != nil {
				route.Observe(publichealth.Observation{Success: true, Latency: latency, ConnectLatency: latency})
			}
			break
		}
		if route.Observe != nil {
			route.Observe(publichealth.Observation{Success: false, Latency: latency, ConnectLatency: latency, Cause: errors.Join(err, r.Context().Err())})
		}
		recordTunnel(h.recorder, r.Context(), ctx, route, host, "connect", http.StatusBadGateway, 0, 0, 0, 0)
		if route.Retry == nil || attempt >= retryPolicy.MaxAttempts || retrypkg.Wait(r.Context(), attempt) != nil {
			break
		}
		next, retryErr := route.Retry(r.Context())
		if retryErr != nil || next.Dial == nil || internalbudget.Available(r.Context(), next.Reserve) != nil || next.Acquire != nil && !next.Acquire() {
			break
		}
		next.PolicyID, next.RuleID = result.PolicyID, result.TerminalRuleID
		route = next
		attempt++
	}
	if err != nil {
		http.Error(w, "UPSTREAM_FAILED", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstream.Close() }()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "CONNECT_UNAVAILABLE", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffer.Flush(); err != nil {
		return
	}
	var copies sync.WaitGroup
	clientUpload := &internaltraffic.Reader{Source: buffer.Reader}
	upstreamUpload := &internaltraffic.Writer{Destination: &internalbudget.Writer{Context: r.Context(), Destination: upstream, Reserve: route.Reserve}}
	upstreamDownload := &internaltraffic.Reader{Source: &internalbudget.Reader{Context: r.Context(), Source: upstream, Reserve: route.Reserve}}
	clientDownload := &internaltraffic.Writer{Destination: client}
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(upstreamUpload, clientUpload)
		if closeWriter, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = closeWriter.CloseWrite()
		}
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(clientDownload, upstreamDownload)
		if closeWriter, ok := client.(interface{ CloseWrite() error }); ok {
			_ = closeWriter.CloseWrite()
		}
	}()
	copies.Wait()
	recordTunnel(h.recorder, r.Context(), ctx, route, host, "connect", http.StatusOK, clientUpload.Bytes(), clientDownload.Bytes(), upstreamUpload.Bytes(), upstreamDownload.Bytes())
}
func defaultPort(scheme string) int {
	if scheme == "https" {
		return 443
	}
	if scheme == "http" {
		return 80
	}
	return 0
}
func cloneHeader(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}
func copyHeader(dst, src http.Header) {
	for k, v := range src {
		for _, value := range v {
			dst.Add(k, value)
		}
	}
}
func stripHopByHop(h http.Header) {
	for _, line := range h.Values("Connection") {
		for _, key := range strings.Split(line, ",") {
			h.Del(strings.TrimSpace(key))
		}
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
	for key := range h {
		if strings.HasPrefix(strings.ToLower(key), "x-proxysieve-") {
			h.Del(key)
		}
	}
}

type countedBody struct {
	io.Reader
	io.Closer
}

func cacheKeyFor(route gateway.Route, requestContext policy.RequestContext, request *http.Request) cachepkg.Key {
	routeID := route.PoolID
	if routeID == "" {
		routeID = "direct"
	}
	sessionHash := route.SessionHash
	if sessionHash == "" {
		sessionHash = "none"
	}
	return cachepkg.Key{ClientID: requestContext.ClientID, SessionHash: sessionHash, RouteID: routeID, Method: request.Method, URL: request.URL.String(), Vary: map[string]string{}}
}
func recordHTTP(recorder trafficpkg.Recorder, request *http.Request, ctx policy.RequestContext, route gateway.Route, host string, upload, download, delivered trafficpkg.Bytes, status int, cacheServed trafficpkg.Bytes) {
	completeRoute(request.Context(), route, upload, download)
	if recorder == nil {
		return
	}
	direct := trafficpkg.Bytes(0)
	proxyUpload, proxyDownload := trafficpkg.Bytes(0), trafficpkg.Bytes(0)
	switch route.Action {
	case "direct":
		direct = upload + download
	case "proxy", "chain":
		proxyUpload, proxyDownload = upload, download
	}
	cost, _ := trafficpkg.NewCostSnapshot(route.Rate, proxyUpload, proxyDownload)
	_ = recorder.Record(context.WithoutCancel(request.Context()), trafficpkg.Event{At: time.Now().UTC(), RequestID: ctx.RequestID, ConnectionID: ctx.ConnectionID, ClientID: ctx.ClientID, PolicyID: route.PolicyID, RuleID: route.RuleID, PoolID: route.PoolID, ProxyID: route.ProxyID, ChainID: route.ChainID, Host: host, Protocol: "http", Action: route.Action, StatusCode: status, ClientUpload: upload, ClientDownload: delivered, UpstreamUpload: proxyUpload, UpstreamDownload: proxyDownload, Direct: direct, CacheServed: cacheServed, ConfiguredCost: cost})
}

func recordTunnel(recorder trafficpkg.Recorder, recordContext context.Context, request policy.RequestContext, route gateway.Route, host, protocol string, status int, clientUpload, clientDownload, routeUpload, routeDownload trafficpkg.Bytes) {
	completeRoute(recordContext, route, routeUpload, routeDownload)
	if recorder == nil {
		return
	}
	direct := trafficpkg.Bytes(0)
	proxyUpload, proxyDownload := trafficpkg.Bytes(0), trafficpkg.Bytes(0)
	switch route.Action {
	case "direct":
		direct, _ = routeUpload.Add(routeDownload)
	case "proxy", "chain":
		proxyUpload, proxyDownload = routeUpload, routeDownload
	}
	cost, _ := trafficpkg.NewCostSnapshot(route.Rate, proxyUpload, proxyDownload)
	_ = recorder.Record(context.WithoutCancel(recordContext), trafficpkg.Event{
		At: time.Now().UTC(), RequestID: request.RequestID, ConnectionID: request.ConnectionID,
		ClientID: request.ClientID, PolicyID: route.PolicyID, RuleID: route.RuleID, PoolID: route.PoolID, ProxyID: route.ProxyID, ChainID: route.ChainID, Host: host,
		Protocol: protocol, Action: route.Action, StatusCode: status,
		ClientUpload: clientUpload, ClientDownload: clientDownload,
		UpstreamUpload: proxyUpload, UpstreamDownload: proxyDownload, Direct: direct, ConfiguredCost: cost,
	})
}

func completeRoute(ctx context.Context, route gateway.Route, upload, download trafficpkg.Bytes) {
	if route.Complete != nil {
		route.Complete(context.WithoutCancel(ctx), upload, download)
	}
}

type attemptTrace struct {
	mu             sync.Mutex
	started        time.Time
	connectStarted map[string]time.Time
	connectLatency time.Duration
	ttfb           time.Duration
	dnsFailure     bool
	tlsFailure     bool
}

func newAttemptTrace(started time.Time) *attemptTrace {
	return &attemptTrace{started: started, connectStarted: make(map[string]time.Time)}
}

func (t *attemptTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone: func(info httptrace.DNSDoneInfo) {
			t.mu.Lock()
			t.dnsFailure = t.dnsFailure || info.Err != nil
			t.mu.Unlock()
		},
		ConnectStart: func(network, address string) {
			t.mu.Lock()
			t.connectStarted[network+"\x00"+address] = time.Now()
			t.mu.Unlock()
		},
		ConnectDone: func(network, address string, err error) {
			t.mu.Lock()
			key := network + "\x00" + address
			started := t.connectStarted[key]
			delete(t.connectStarted, key)
			if !started.IsZero() && (err == nil || t.connectLatency == 0) {
				t.connectLatency = time.Since(started)
			}
			t.mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			t.mu.Lock()
			t.tlsFailure = t.tlsFailure || err != nil
			t.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			if t.ttfb == 0 {
				t.ttfb = time.Since(t.started)
			}
			t.mu.Unlock()
		},
	}
}

func (t *attemptTrace) observation(success bool, status int, latency time.Duration, cause error) publichealth.Observation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return publichealth.Observation{
		Success: success, HTTPStatus: status, Latency: latency, Cause: cause,
		ConnectLatency: t.connectLatency, TTFB: t.ttfb,
		DNSFailure: t.dnsFailure, TLSFailure: t.tlsFailure,
	}
}
func (h *Handler) authenticateRequest(w http.ResponseWriter, r *http.Request) (model.ID, bool) {
	if h.authenticatePassword != nil {
		username, password, ok := basicCredentials(r.Header.Get("Proxy-Authorization"))
		if !ok {
			w.Header().Set("Proxy-Authenticate", `Basic realm="ProxySieve"`)
			http.Error(w, "PROXY_AUTH_REQUIRED", http.StatusProxyAuthRequired)
			return "", false
		}
		clientID, err := h.authenticatePassword(r.Context(), username, password)
		if err != nil {
			w.Header().Set("Proxy-Authenticate", `Basic realm="ProxySieve"`)
			http.Error(w, "PROXY_AUTH_REQUIRED", http.StatusProxyAuthRequired)
			return "", false
		}
		return clientID, true
	}
	if h.authenticate == nil {
		return "local-http", true
	}
	id, err := h.authenticate(r.Context(), r.Header.Get("Proxy-Authorization"))
	if err != nil {
		w.Header().Set("Proxy-Authenticate", "Bearer")
		http.Error(w, "PROXY_AUTH_REQUIRED", http.StatusProxyAuthRequired)
		return "", false
	}
	return id, true
}

func basicCredentials(header string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") || len(parts[1]) > 8192 {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}
	decodedText := string(decoded)
	for i := range decoded {
		decoded[i] = 0
	}
	username, password, ok := strings.Cut(decodedText, ":")
	if !ok || username == "" || password == "" || len(username) > 255 || len(password) > 255 {
		return "", "", false
	}
	return username, password, true
}

var _ http.Handler = (*Handler)(nil)
