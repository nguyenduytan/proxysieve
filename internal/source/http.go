// Package source contains safe, bounded external proxy-source refresh primitives.
package source

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

var ErrFetch = errors.New("proxy source fetch failed")
var ErrRedirect = errors.New("proxy source redirect rejected")

const MaxBodyBytes = 8 << 20

type Resolver interface {
	LookupNetIP(context.Context, string) ([]netip.Addr, error)
}

// FetchHTTP accepts an explicit absolute HTTP(S) source URL. It pins the resolved
// address during dial, refuses redirects, carries no ambient credentials/cookies,
// and bounds response memory. Callers should parse/preview before persistence.
func FetchHTTP(ctx context.Context, raw string, resolver Resolver, policy security.DestinationPolicy) ([]byte, error) {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "http" && target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
		return nil, ErrFetch
	}
	if resolver == nil {
		return nil, ErrFetch
	}
	addresses, err := resolver.LookupNetIP(ctx, target.Hostname())
	if err != nil || policy.Allow(addresses) != nil {
		return nil, ErrFetch
	}
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, DisableCompression: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, requestedPort, err := net.SplitHostPort(address)
		if err != nil || requestedPort != port || !sameHost(host, target.Hostname()) {
			return nil, ErrFetch
		}
		dialer := net.Dialer{Timeout: 15 * time.Second}
		var last error
		for _, ip := range addresses {
			connection, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return connection, nil
			}
			last = e
		}
		return nil, last
	}}
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ErrFetch
	}
	request.Header.Set("Accept", "text/plain, application/json;q=0.9, text/csv;q=0.8")
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrFetch
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, ErrFetch
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxBodyBytes+1))
	if err != nil || len(body) > MaxBodyBytes {
		return nil, ErrFetch
	}
	return body, nil
}

// PreviewHTTP fetches a source through the same SSRF controls, then uses the
// standard parser preview without persisting a single endpoint.
func PreviewHTTP(ctx context.Context, raw string, resolver Resolver, policy security.DestinationPolicy, maxLines int) (proxy.ImportPreview, error) {
	body, err := FetchHTTP(ctx, raw, resolver, policy)
	if err != nil {
		return proxy.ImportPreview{}, err
	}
	return proxy.Preview(string(body), maxLines)
}
func sameHost(left, right string) bool {
	if leftIP, err := netip.ParseAddr(left); err == nil {
		if rightIP, e := netip.ParseAddr(right); e == nil {
			return leftIP.Unmap() == rightIP.Unmap()
		}
	}
	return strings.EqualFold(strings.TrimSuffix(left, "."), strings.TrimSuffix(right, "."))
}
func AddressForURL(target *url.URL) string {
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(target.Hostname(), strconv.Itoa(mustPort(port)))
}
func mustPort(raw string) int { value, _ := strconv.Atoi(raw); return value }
