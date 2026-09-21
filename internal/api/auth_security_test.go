package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"go.yaml.in/yaml/v3"
)

func TestRejectsCrossOriginAndTrailingJSON(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	for _, tc := range []struct {
		origin, body, contentType string
		want                      int
	}{
		{"https://evil.example", `{"username":"x","password":"y"}`, "application/json", 403},
		{"http://127.0.0.1:9999", `{}`, "application/json", 403},
		{"", `{} {}`, "application/json", 400},
		{"", `{}`, "text/plain", 415},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(tc.body))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("origin %q: %d, want %d", tc.origin, w.Code, tc.want)
		}
	}
}
func TestSessionBoundCSRFAndMethodGuards(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	token, _ := service.SetupToken(t.Context())
	setup := request(server.Handler(), http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake password"}, "")
	cookies := cookiesFor(setup)
	w := requestWithCSRF(server.Handler(), cookies, "forged-cookie-pair")
	if w.Code != 403 {
		t.Fatal("forged CSRF accepted", w.Code)
	}
	if request(server.Handler(), http.MethodGet, "/api/v1/auth/me", nil, cookies).Code != 200 {
		t.Fatal("failed logout revoked session")
	}
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		r := httptest.NewRequest(method, "/api/v1/proxies", nil)
		r.Host = "127.0.0.1"
		r.Header.Set("Cookie", cookies)
		r.Header.Set("X-CSRF-Token", cookieValueFrom(cookies, csrfCookie))
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatal(method, w.Code)
		}
	}
}
func TestLoginRateLimitAndAssetFallback(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	for i := 0; i < 30; i++ {
		if w := request(server.Handler(), http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "missing", "password": "not a real password"}, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request(server.Handler(), http.MethodPost, "/api/v1/auth/login", map[string]string{}, ""); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code)
	}
	if w := request(server.Handler(), http.MethodGet, "/proxies", nil, ""); w.Code != 200 || w.Header().Get("Location") != "" {
		t.Fatal("SPA fallback redirect", w.Code)
	}
	if w := request(server.Handler(), http.MethodGet, "/assets/missing.js", nil, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestOpenAPIContractIsServedWithoutSession(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	response := request(server.Handler(), http.MethodGet, "/api/v1/openapi.yaml", nil, "")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/yaml; charset=utf-8" {
		t.Fatal(response.Code, response.Header())
	}
	if !strings.Contains(response.Body.String(), "openapi: 3.1.0") || !strings.Contains(response.Body.String(), "/api/v1/clients/{clientId}/api-keys/{keyId}") || !strings.Contains(response.Body.String(), "/api/v1/sources/{sourceId}") || !strings.Contains(response.Body.String(), "/api/v1/browser/policy") || !strings.Contains(response.Body.String(), "/api/v1/browser/blocks") || !strings.Contains(response.Body.String(), "/api/v1/webhooks/{webhookId}/deliveries") {
		t.Fatal("served OpenAPI contract is incomplete")
	}
}

func TestOpenAPIContractParsesAsYAML(t *testing.T) {
	var document map[string]any
	if err := yaml.Unmarshal(openAPISpec, &document); err != nil {
		t.Fatal(err)
	}
	if document["openapi"] != "3.1.0" {
		t.Fatalf("unexpected OpenAPI version: %#v", document["openapi"])
	}
	if _, ok := document["paths"]; !ok {
		t.Fatal("OpenAPI document has no paths")
	}
}
