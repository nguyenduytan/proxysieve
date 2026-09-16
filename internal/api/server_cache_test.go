package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publiccache "github.com/nguyenduytan/proxysieve/pkg/cache"
)

func TestCacheStatsAndPurgeAuthorization(t *testing.T) {
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	audits, _ := audit.NewMemory(10)
	responseCache, _ := internalcache.NewMemory(2, 32)
	now := time.Now().UTC()
	responseCache.Put(publiccache.Key{ClientID: "client", SessionHash: "hash", RouteID: "route", Method: "GET", URL: "https://example.invalid"}, internalcache.Entry{Body: []byte("cached"), ExpiresAt: now.Add(time.Minute)})
	responseCache.Put(publiccache.Key{ClientID: "client", SessionHash: "hash", RouteID: "route", Method: "GET", URL: "https://other.invalid"}, internalcache.Entry{Body: []byte("other"), ExpiresAt: now.Add(time.Minute)})
	server, _ := New(service, nil, nil, audits)
	server.SetCache(responseCache)
	handler := server.Handler()

	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: now})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)
	stats := request(handler, http.MethodGet, "/api/v1/cache/stats", nil, viewerCookies)
	if stats.Code != http.StatusOK || stats.Body.String() != "{\"enabled\":true,\"stats\":{\"entries\":2,\"bytes_stored\":11,\"max_entries\":2,\"max_bytes\":32,\"hits\":0,\"misses\":0,\"bypasses\":0,\"expired\":0,\"evictions\":0,\"bytes_served\":0,\"hit_ratio\":0}}\n" {
		t.Fatal(stats.Code, stats.Body.String())
	}
	if forbidden := mutationRequest(handler, http.MethodPost, "/api/v1/cache/purge", nil, viewerCookies); forbidden.Code != http.StatusForbidden {
		t.Fatal("viewer purged cache", forbidden.Code, forbidden.Body.String())
	}

	operatorToken, _ := service.CreateSession(auth.User{ID: "operator", Username: "operator", Role: auth.RoleOperator, Enabled: true, CreatedAt: now})
	operatorCookies := sessionCookie + "=" + operatorToken + "; " + csrfCookie + "=" + service.CSRFToken(operatorToken)
	if invalid := mutationRequest(handler, http.MethodPost, "/api/v1/cache/purge/domain", map[string]any{"domain": "example.invalid:443"}, operatorCookies); invalid.Code != http.StatusBadRequest {
		t.Fatal("invalid domain accepted", invalid.Code, invalid.Body.String())
	}
	domainPurged := mutationRequest(handler, http.MethodPost, "/api/v1/cache/purge/domain", map[string]any{"domain": "EXAMPLE.INVALID."}, operatorCookies)
	if domainPurged.Code != http.StatusOK || !containsAll(domainPurged.Body.String(), `"domain":"example.invalid"`, `"entries":1`, `"bytes":6`) || responseCache.Stats(now).Entries != 1 {
		t.Fatal(domainPurged.Code, domainPurged.Body.String())
	}
	if wrongMethod := mutationRequest(handler, http.MethodDelete, "/api/v1/cache/purge", nil, operatorCookies); wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatal("delete purge accepted", wrongMethod.Code, wrongMethod.Body.String())
	}
	purged := mutationRequest(handler, http.MethodPost, "/api/v1/cache/purge", nil, operatorCookies)
	if purged.Code != http.StatusOK || responseCache.Stats(now).Entries != 0 {
		t.Fatal(purged.Code, purged.Body.String())
	}
	events, err := audits.ListAudit(context.Background(), audit.Page{Limit: 10})
	if err != nil || len(events) != 2 || !hasCacheAudit(events, "cache.purged", "response") || !hasCacheAudit(events, "cache.domain_purged", "example.invalid") {
		t.Fatal(events, err)
	}
}

func hasCacheAudit(events []audit.Event, action, targetID string) bool {
	for _, event := range events {
		if event.Action == action && event.TargetID == targetID && event.ActorID == "operator" {
			return true
		}
	}
	return false
}

func TestCacheStatsExplicitlyReportsDisabled(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).cacheStats(recorder)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"enabled\":false}\n" {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
}
