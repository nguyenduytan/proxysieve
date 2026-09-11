package httpforward

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
