package cache

import (
	"net/http"
	"testing"
)

func TestEligibility(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if !CheckRequest(request).Eligible {
		t.Fatal("get rejected")
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Method = http.MethodPost }, func(r *http.Request) { r.Header.Set("Authorization", "Bearer fake") }, func(r *http.Request) { r.Header.Set("Cookie", "a=b") }, func(r *http.Request) { r.Header.Set("Cache-Control", "no-store") }} {
		copy := request.Clone(request.Context())
		mutate(copy)
		if CheckRequest(copy).Eligible {
			t.Fatal("unsafe request accepted")
		}
	}
	headers := http.Header{}
	if !CheckResponse(200, headers, 10, 100).Eligible {
		t.Fatal("response rejected")
	}
	headers.Set("Set-Cookie", "x=y")
	if CheckResponse(200, headers, 10, 100).Eligible {
		t.Fatal("cookie response")
	}
	headers.Del("Set-Cookie")
	headers.Set("Vary", "*")
	if CheckResponse(200, headers, 10, 100).Eligible {
		t.Fatal("vary accepted")
	}
}
func TestKeyIsStableAndPartitioned(t *testing.T) {
	a := Key{ClientID: "client", SessionHash: "session", RouteID: "route", Method: "GET", URL: "https://example.invalid", Vary: map[string]string{"Accept": "json", "Accept-Language": "en"}}
	b := a
	b.Vary = map[string]string{"Accept-Language": "en", "Accept": "json"}
	if a.String() != b.String() {
		t.Fatal("unordered vary key")
	}
	b.SessionHash = "other"
	if a.String() == b.String() {
		t.Fatal("session omitted")
	}
}
