package httpforward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/gateway"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

func TestHopByHopAndIntegrationMetadata(t *testing.T) {
	h := http.Header{"Connection": {"X-Internal, Keep-Alive"}, "X-Internal": {"private"}, "Keep-Alive": {"timeout=5"}, "Proxy-Authorization": {"fake"}, "X-Proxysieve-Session": {"private"}, "Authorization": {"origin-only"}, "Accept": {"text/plain"}}
	stripHopByHop(h)
	for _, key := range []string{"Connection", "X-Internal", "Keep-Alive", "Proxy-Authorization", "X-Proxysieve-Session"} {
		if h.Get(key) != "" {
			t.Fatal("header leaked", key)
		}
	}
	if h.Get("Authorization") != "origin-only" || h.Get("Accept") != "text/plain" {
		t.Fatal("origin headers stripped")
	}
}
func TestExplicitInvalidPortsAreNotDefaulted(t *testing.T) {
	h, _ := New(Options{Evaluator: Decider(direct), Router: RouterFunc(routeDirect)})
	for _, target := range []string{"http://example.invalid:0/", "http://example.invalid:65536/", "ftp://example.invalid:80/", "http://example.invalid:/"} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(target, w.Code)
		}
	}
}

func TestSessionHeaderStaysInternal(t *testing.T) {
	var visible string
	var sessionKey string
	h, err := New(Options{
		Evaluator: Decider(func(_ context.Context, request policy.RequestContext, _ policy.Visibility) (policy.Result, error) {
			if values := request.Headers.Value["X-ProxySieve-Session"]; len(values) > 0 {
				visible = values[0]
			}
			sessionKey = request.SessionKey
			return policy.Result{Actions: []policy.Action{{Type: "direct"}}}, nil
		}),
		Router: RouterFunc(func(context.Context, policy.RequestContext, policy.Result) (gateway.Route, error) {
			return gateway.Route{Action: "direct", Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if got := request.Header.Get("X-ProxySieve-Session"); got != "" {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(http.NoBody), Request: request}, nil
			})}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	request.Header.Set("X-ProxySieve-Session", "private-key")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || visible != "" || sessionKey != "private-key" {
		t.Fatalf("session metadata leaked or missing: status=%d visible=%q key=%q", response.Code, visible, sessionKey)
	}
}

func TestCacheKeyPartitionsClientAndSession(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	left := cacheKeyFor(gateway.Route{PoolID: "pool", SessionHash: "session-a"}, policy.RequestContext{ClientID: "client-a"}, request)
	right := cacheKeyFor(gateway.Route{PoolID: "pool", SessionHash: "session-b"}, policy.RequestContext{ClientID: "client-b"}, request)
	if reflect.DeepEqual(left, right) {
		t.Fatal("cache key is not isolated by client and session")
	}
}
