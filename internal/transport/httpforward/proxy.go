// Package httpforward implements the visible HTTP forward-proxy adapter.
package httpforward

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	cachepkg "github.com/nguyenduytan/proxysieve/pkg/cache"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	trafficpkg "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type Decider func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)

func (f Decider) Evaluate(ctx context.Context, r policy.RequestContext, v policy.Visibility) (policy.Result, error) {
	return f(ctx, r, v)
}

type RouterFunc func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error)

func (f RouterFunc) Route(ctx context.Context, r policy.RequestContext, result policy.Result) (gateway.Route, error) {
	return f(ctx, r, result)
}

type Options struct {
	Evaluator      gateway.Evaluator
	Router         gateway.Router
	Recorder       trafficpkg.Recorder
	ResponseCache  *internalcache.Memory
	MaxCacheBody   int64
	MaxHeaderBytes int
}
type Handler struct {
	evaluator      gateway.Evaluator
	router         gateway.Router
	recorder       trafficpkg.Recorder
	responseCache  *internalcache.Memory
	maxCacheBody   int64
	maxHeaderBytes int
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
	return &Handler{evaluator: options.Evaluator, router: options.Router, recorder: options.Recorder, responseCache: options.ResponseCache, maxCacheBody: options.MaxCacheBody, maxHeaderBytes: options.MaxHeaderBytes}, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.connect(w, r)
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
	ctx := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), Listener: "http", Protocol: "http", Scheme: r.URL.Scheme, Host: host, Port: port, Method: model.Optional[string]{Known: true, Value: r.Method}, Path: model.Optional[string]{Known: true, Value: r.URL.EscapedPath()}, Headers: model.Optional[map[string][]string]{Known: true, Value: cloneHeader(r.Header)}, Timestamp: time.Now().UTC()}
	result, err := h.evaluator.Evaluate(r.Context(), ctx, policy.Visibility{Host: true, Method: true, Path: true, Headers: true})
	if err != nil {
		http.Error(w, "POLICY_REJECTED", http.StatusForbidden)
		return
	}
	route, err := h.router.Route(r.Context(), ctx, result)
	if err != nil || route.Action == "block" || route.Action == "reject" || route.Action == "" {
		if errors.Is(err, gateway.ErrDenied) {
			http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
			return
		}
		http.Error(w, "POLICY_BLOCKED", http.StatusForbidden)
		return
	}
	if route.Transport == nil {
		http.Error(w, "ROUTE_UNAVAILABLE", http.StatusBadGateway)
		return
	}
	cacheKey := cacheKeyFor(route, r)
	if h.responseCache != nil && cachepkg.CheckRequest(r).Eligible {
		if cached, ok := h.responseCache.Get(cacheKey, time.Now().UTC()); ok {
			copyHeader(w.Header(), http.Header(cached.Header))
			w.WriteHeader(cached.Status)
			delivered := &internaltraffic.Writer{Destination: w}
			_, copyErr := delivered.Write(cached.Body)
			if h.recorder != nil {
				_ = h.recorder.Record(context.WithoutCancel(r.Context()), trafficpkg.Event{At: time.Now().UTC(), RequestID: ctx.RequestID, ConnectionID: ctx.ConnectionID, PoolID: route.PoolID, ProxyID: route.ProxyID, Host: host, Protocol: "http", Action: "cache", StatusCode: cached.Status, ClientDownload: delivered.Bytes(), CacheServed: delivered.Bytes()})
			}
			if copyErr != nil {
				panic(http.ErrAbortHandler)
			}
			return
		}
	}
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	upload := &internaltraffic.Reader{Source: body}
	out := r.Clone(r.Context())
	out.Body = &countedBody{Reader: upload, Closer: body}
	if body == http.NoBody {
		out.Body = http.NoBody
	}
	out.RequestURI = ""
	out.Host = r.URL.Host
	out.Header = cloneHeader(r.Header)
	stripHopByHop(out.Header)
	out.Header.Del("Proxy-Authorization")
	out.Header.Del("Proxy-Connection")
	response, err := route.Transport.RoundTrip(out)
	if err != nil {
		if route.Observe != nil {
			route.Observe(false, 0)
		}
		http.Error(w, "UPSTREAM_FAILED", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if route.Observe != nil {
		route.Observe(true, response.StatusCode)
	}
	download := &internaltraffic.Reader{Source: response.Body}
	responseHeaders := response.Header.Clone()
	stripHopByHop(responseHeaders)
	cacheable := h.responseCache != nil && cachepkg.CheckRequest(r).Eligible && response.Header.Get("Vary") == "" && cachepkg.CheckResponse(response.StatusCode, response.Header, response.ContentLength, h.maxCacheBody).Eligible
	if cacheable {
		body, readErr := io.ReadAll(io.LimitReader(download, h.maxCacheBody+1))
		if readErr == nil && int64(len(body)) <= h.maxCacheBody {
			_ = h.responseCache.Put(cacheKey, internalcache.Entry{Status: response.StatusCode, Header: responseHeaders, Body: body, ExpiresAt: time.Now().UTC().Add(time.Minute)})
			copyHeader(w.Header(), responseHeaders)
			w.WriteHeader(response.StatusCode)
			delivered := &internaltraffic.Writer{Destination: w}
			_, copyErr := delivered.Write(body)
			if h.recorder != nil {
				recordHTTP(h.recorder, r, ctx, route, host, upload.Bytes(), download.Bytes(), delivered.Bytes(), response.StatusCode, 0)
			}
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
	if h.recorder != nil {
		recordHTTP(h.recorder, r, ctx, route, host, upload.Bytes(), download.Bytes(), delivered.Bytes(), response.StatusCode, 0)
	}
	if copyErr != nil {
		panic(http.ErrAbortHandler)
	}
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
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
	ctx := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), Listener: "http", Protocol: "connect", Host: host, Port: uint16(port64), Method: model.Optional[string]{Known: true, Value: http.MethodConnect}, Timestamp: time.Now().UTC()}
	result, err := h.evaluator.Evaluate(r.Context(), ctx, policy.Visibility{Host: true, Method: true})
	if err != nil {
		http.Error(w, "POLICY_BLOCKED", http.StatusForbidden)
		return
	}
	route, err := h.router.Route(r.Context(), ctx, result)
	if err != nil || route.Dial == nil {
		if errors.Is(err, gateway.ErrDenied) {
			http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
			return
		}
		http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
		return
	}
	upstream, err := route.Dial(r.Context(), net.JoinHostPort(host, portRaw))
	if err != nil {
		if route.Observe != nil {
			route.Observe(false, 0)
		}
		http.Error(w, "UPSTREAM_FAILED", http.StatusBadGateway)
		return
	}
	if route.Observe != nil {
		route.Observe(true, 0)
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
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(upstream, buffer.Reader)
		if closeWriter, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = closeWriter.CloseWrite()
		}
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(client, upstream)
		if closeWriter, ok := client.(interface{ CloseWrite() error }); ok {
			_ = closeWriter.CloseWrite()
		}
	}()
	copies.Wait()
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

func cacheKeyFor(route gateway.Route, request *http.Request) cachepkg.Key {
	routeID := route.PoolID
	if routeID == "" {
		routeID = "direct"
	}
	return cachepkg.Key{ClientID: "local", SessionHash: "anonymous", RouteID: routeID, Method: request.Method, URL: request.URL.String(), Vary: map[string]string{}}
}
func recordHTTP(recorder trafficpkg.Recorder, request *http.Request, ctx policy.RequestContext, route gateway.Route, host string, upload, download, delivered trafficpkg.Bytes, status int, cacheServed trafficpkg.Bytes) {
	direct := trafficpkg.Bytes(0)
	proxyUpload, proxyDownload := trafficpkg.Bytes(0), trafficpkg.Bytes(0)
	switch route.Action {
	case "direct":
		direct = upload + download
	case "proxy":
		proxyUpload, proxyDownload = upload, download
	}
	_ = recorder.Record(context.WithoutCancel(request.Context()), trafficpkg.Event{At: time.Now().UTC(), RequestID: ctx.RequestID, ConnectionID: ctx.ConnectionID, PoolID: route.PoolID, ProxyID: route.ProxyID, Host: host, Protocol: "http", Action: route.Action, StatusCode: status, ClientUpload: upload, ClientDownload: delivered, UpstreamUpload: proxyUpload, UpstreamDownload: proxyDownload, Direct: direct, CacheServed: cacheServed})
}

var _ http.Handler = (*Handler)(nil)
