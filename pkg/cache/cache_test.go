package cache

import (
	"net/http"
	"testing"
	"time"
)

func TestEligibility(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if !CheckRequest(request).Eligible {
		t.Fatal("get rejected")
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Method = http.MethodPost }, func(r *http.Request) { r.Header.Set("Authorization", "Bearer fake") }, func(r *http.Request) { r.Header.Set("Cookie", "a=b") }, func(r *http.Request) { r.Header.Set("Cache-Control", "no-store") }, func(r *http.Request) { r.Header.Set("Cache-Control", "no-cache") }, func(r *http.Request) { r.Header.Set("Cache-Control", "max-age = 0") }, func(r *http.Request) { r.Header.Set("Pragma", "no-cache") }} {
		copy := request.Clone(request.Context())
		mutate(copy)
		if CheckRequest(copy).Eligible {
			t.Fatal("unsafe request accepted")
		}
	}
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	headers := http.Header{"Cache-Control": {"max-age=60"}}
	result := CheckResponse(200, headers, 10, 100, now)
	if !result.Eligible || !result.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatal("response rejected")
	}
	headers.Set("Set-Cookie", "x=y")
	if CheckResponse(200, headers, 10, 100, now).Eligible {
		t.Fatal("cookie response")
	}
	headers.Del("Set-Cookie")
	headers.Set("Vary", "Accept-Encoding")
	if CheckResponse(200, headers, 10, 100, now).Eligible {
		t.Fatal("vary accepted")
	}
}

func TestResponseFreshness(t *testing.T) {
	now := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		headers http.Header
		want    time.Time
	}{
		{name: "shared max age and age", headers: http.Header{"Cache-Control": {`max-age=300, s-maxage="120"`}, "Age": {"20"}}, want: now.Add(100 * time.Second)},
		{name: "expires", headers: http.Header{"Expires": {now.Add(time.Hour).Format(http.TimeFormat)}}, want: now.Add(time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckResponse(http.StatusOK, tc.headers, 10, 100, now)
			if !got.Eligible || !got.ExpiresAt.Equal(tc.want) {
				t.Fatalf("unexpected freshness: %+v", got)
			}
		})
	}
	for _, headers := range []http.Header{
		{},
		{"Cache-Control": {"no-cache, max-age=60"}},
		{"Cache-Control": {"max-age=0"}},
		{"Cache-Control": {"max-age=60"}, "Age": {"60"}},
		{"Expires": {now.Format(http.TimeFormat)}},
	} {
		if got := CheckResponse(http.StatusOK, headers, 10, 100, now); got.Eligible {
			t.Fatalf("unsafe freshness accepted: %#v", headers)
		}
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
