package source

import (
	"context"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

type resolverFunc func(context.Context, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return f(ctx, host)
}
func TestFetchHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Fatal("cookie forwarded")
		}
		_, _ = w.Write([]byte("proxy.example.invalid:8080"))
	}))
	defer server.Close()
	body, err := FetchHTTP(context.Background(), server.URL, resolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}), security.DestinationPolicy{AllowTrusted: true})
	if err != nil || string(body) != "proxy.example.invalid:8080" {
		t.Fatal(string(body), err)
	}
}
func TestPreviewHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxy.example.invalid:8080\nbad"))
	}))
	defer server.Close()
	preview, err := PreviewHTTP(context.Background(), server.URL, resolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}), security.DestinationPolicy{AllowTrusted: true}, 10)
	if err != nil || preview.Valid != 1 || preview.Invalid != 1 {
		t.Fatal(preview, err)
	}
}
func TestFetchRejectsRedirectPrivateAndSize(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid", http.StatusFound)
	}))
	defer redirect.Close()
	resolver := resolverFunc(func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	if _, err := FetchHTTP(context.Background(), redirect.URL, resolver, security.DestinationPolicy{AllowTrusted: true}); err == nil {
		t.Fatal("redirect accepted")
	}
	if _, err := FetchHTTP(context.Background(), "http://example.invalid", resolver, security.DestinationPolicy{DenyPrivate: true}); err == nil {
		t.Fatal("private resolution accepted")
	}
	if _, err := FetchHTTP(context.Background(), "ftp://example.invalid", resolver, security.DestinationPolicy{AllowTrusted: true}); err == nil {
		t.Fatal("scheme accepted")
	}
}
