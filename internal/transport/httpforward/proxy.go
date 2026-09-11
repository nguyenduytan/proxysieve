// Package httpforward implements the visible HTTP forward-proxy adapter.
package httpforward

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

type ResolverFunc func(context.Context, string) ([]netip.Addr, error)

func (f ResolverFunc) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return f(ctx, host)
}

type Decider func(context.Context, policy.RequestContext, policy.Visibility) (policy.Result, error)

func (f Decider) Evaluate(ctx context.Context, r policy.RequestContext, v policy.Visibility) (policy.Result, error) {
	return f(ctx, r, v)
}

type Options struct {
	Evaluator         gateway.Evaluator
	Resolver          security.Resolver
	DestinationPolicy security.DestinationPolicy
	Transport         http.RoundTripper
	MaxHeaderBytes    int
}
type Handler struct {
	evaluator      gateway.Evaluator
	resolver       security.Resolver
	policy         security.DestinationPolicy
	transport      http.RoundTripper
	maxHeaderBytes int
}

func New(options Options) (*Handler, error) {
	if options.Evaluator == nil || options.Resolver == nil {
		return nil, errors.New("http forward proxy dependencies are required")
	}
	if options.Transport == nil {
		options.Transport = &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 64, MaxIdleConnsPerHost: 8}
	}
	if options.MaxHeaderBytes == 0 {
		options.MaxHeaderBytes = 32 << 10
	}
	if options.MaxHeaderBytes < 1024 || options.MaxHeaderBytes > 1<<20 {
		return nil, errors.New("invalid header limit")
	}
	return &Handler{evaluator: options.Evaluator, resolver: options.Resolver, policy: options.DestinationPolicy, transport: options.Transport, maxHeaderBytes: options.MaxHeaderBytes}, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.connect(w, r)
		return
	}
	if r.URL == nil || !r.URL.IsAbs() || r.URL.Host == "" || r.URL.User != nil || r.Host == "" {
		http.Error(w, "INVALID_PROXY_REQUEST", http.StatusBadRequest)
		return
	}
	if r.ContentLength > 1<<30 {
		http.Error(w, "REQUEST_TOO_LARGE", http.StatusRequestEntityTooLarge)
		return
	}
	host := r.URL.Hostname()
	port := uint16(defaultPort(r.URL.Scheme))
	if p, err := strconv.ParseUint(r.URL.Port(), 10, 16); err == nil && p > 0 {
		port = uint16(p)
	}
	if host == "" || port == 0 {
		http.Error(w, "INVALID_DESTINATION", http.StatusBadRequest)
		return
	}
	ctx := policy.RequestContext{RequestID: model.NewID(), ConnectionID: model.NewID(), Listener: "http", Protocol: "http", Scheme: r.URL.Scheme, Host: host, Port: port, Method: model.Optional[string]{Known: true, Value: r.Method}, Path: model.Optional[string]{Known: true, Value: r.URL.EscapedPath()}, Headers: model.Optional[map[string][]string]{Known: true, Value: cloneHeader(r.Header)}, Timestamp: time.Now().UTC()}
	result, err := h.evaluator.Evaluate(r.Context(), ctx, policy.Visibility{Host: true, Method: true, Path: true, Headers: true})
	if err != nil {
		http.Error(w, "POLICY_REJECTED", http.StatusForbidden)
		return
	}
	action := terminal(result.Actions)
	if action == "block" || action == "reject" || action == "" {
		http.Error(w, "POLICY_BLOCKED", http.StatusForbidden)
		return
	}
	if action != "direct" {
		http.Error(w, "ROUTE_UNAVAILABLE", http.StatusBadGateway)
		return
	}
	addrs, err := h.resolver.LookupNetIP(r.Context(), host)
	if err != nil || h.policy.Allow(addrs) != nil {
		http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Host = r.URL.Host
	out.Header = cloneHeader(r.Header)
	stripHopByHop(out.Header)
	out.Header.Del("Proxy-Authorization")
	out.Header.Del("Proxy-Connection")
	response, err := h.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "UPSTREAM_FAILED", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	copyHeader(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	host, portRaw, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" {
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
	if err != nil || terminal(result.Actions) != "direct" {
		http.Error(w, "POLICY_BLOCKED", http.StatusForbidden)
		return
	}
	addrs, err := h.resolver.LookupNetIP(r.Context(), host)
	if err != nil || h.policy.Allow(addrs) != nil {
		http.Error(w, "DESTINATION_DENIED", http.StatusForbidden)
		return
	}
	var upstream net.Conn
	dialer := net.Dialer{Timeout: 15 * time.Second}
	for _, addr := range addrs {
		upstream, err = dialer.DialContext(r.Context(), "tcp", net.JoinHostPort(addr.String(), portRaw))
		if err == nil {
			break
		}
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
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(upstream, client)
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
func terminal(actions []policy.Action) string {
	for _, a := range actions {
		switch a.Type {
		case "block", "reject", "proxy", "direct", "cache", "mock", "redirect", "rewrite":
			return a.Type
		}
	}
	return ""
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
	for _, key := range append(h.Values("Connection"), "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade") {
		h.Del(strings.TrimSpace(key))
	}
}

var _ http.Handler = (*Handler)(nil)
var _ = net.IPv4len
