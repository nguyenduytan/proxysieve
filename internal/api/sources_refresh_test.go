package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type sourceResolverFunc func(context.Context, string) ([]netip.Addr, error)

func (f sourceResolverFunc) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return f(ctx, host)
}

func TestSourceRefreshIsAtomicRevisionedAndAudited(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Fatal("ambient cookie forwarded to source")
		}
		_, _ = w.Write([]byte("http://one.example.invalid:8080\nbad\nhttp://one.example.invalid:8080"))
	}))
	defer feed.Close()

	handler, server, repository, service, audits, cookies := refreshTestServer(t)
	server.sourceResolver = loopbackResolver()
	server.sourcePolicy = security.DestinationPolicy{AllowTrusted: true}
	now := time.Date(2026, 9, 12, 8, 30, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	source := contract.Source("source")
	source.Config["url"] = feed.URL
	if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
		t.Fatal(err)
	}

	viewerToken, err := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/sources/source/refresh", map[string]int64{"revision": 1}, viewerCookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := request(handler, http.MethodPost, "/api/v1/sources/source/refresh", map[string]int64{"revision": 1}, cookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}

	response := mutationRequest(handler, http.MethodPost, "/api/v1/sources/source/refresh", map[string]int64{"revision": 1}, cookies)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var result struct {
		Source  store.SourceRecord `json:"source"`
		Created int                `json:"created"`
		Updated int                `json:"updated"`
		Skipped int                `json:"skipped"`
		Invalid int                `json:"invalid"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Updated != 0 || result.Skipped != 1 || result.Invalid != 1 || result.Source.Revision != 2 {
		t.Fatalf("unexpected refresh result: %+v", result)
	}
	if !result.Source.Source.LastRefreshAt.Equal(now) || result.Source.Source.LastRefreshStatus != "ok: created=1 updated=0 skipped=1 invalid=1" {
		t.Fatal(result.Source)
	}
	rows, err := repository.List(t.Context(), store.Page{Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].Endpoint.SourceID != source.ID {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}

	response = mutationRequest(handler, http.MethodPost, "/api/v1/sources/source/refresh", map[string]int64{"revision": 2}, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"created":0`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"skipped":2`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	rows, err = repository.List(t.Context(), store.Page{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("duplicate refresh grew inventory: rows=%d err=%v", len(rows), err)
	}
	if stale := mutationRequest(handler, http.MethodPost, "/api/v1/sources/source/refresh", map[string]int64{"revision": 2}, cookies); stale.Code != http.StatusConflict || !bytes.Contains(stale.Body.Bytes(), []byte("SOURCE_CONFLICT")) {
		t.Fatal(stale.Code, stale.Body.String())
	}
	events, err := audits.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	refreshEvents := 0
	for _, event := range events {
		if event.Action == "source.refreshed" {
			refreshEvents++
		}
	}
	if refreshEvents != 2 {
		t.Fatal(events)
	}
}

func TestSourceRefreshRejectsUnsupportedAndUnsafeSources(t *testing.T) {
	var feedCalled atomic.Bool
	feed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { feedCalled.Store(true) }))
	defer feed.Close()
	handler, server, repository, _, _, cookies := refreshTestServer(t)
	server.sourceResolver = loopbackResolver()

	cases := []struct {
		id     model.ID
		change func(*proxy.Source)
		status int
		code   string
	}{
		{id: "disabled", change: func(source *proxy.Source) { source.Enabled = false }, status: http.StatusConflict, code: "SOURCE_DISABLED"},
		{id: "manual", change: func(source *proxy.Source) { source.Type = proxy.ManualSource }, status: http.StatusUnprocessableEntity, code: "SOURCE_REFRESH_UNSUPPORTED"},
		{id: "invalid-url", change: func(source *proxy.Source) { source.Config["url"] = "http://user:password@example.invalid/list" }, status: http.StatusUnprocessableEntity, code: "SOURCE_CONFIG_INVALID"},
		{id: "private", change: func(source *proxy.Source) { source.Config["url"] = feed.URL }, status: http.StatusBadGateway, code: "SOURCE_FETCH_FAILED"},
	}
	for _, test := range cases {
		source := contract.Source(string(test.id))
		test.change(&source)
		if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
			t.Fatal(err)
		}
		response := mutationRequest(handler, http.MethodPost, "/api/v1/sources/"+string(test.id)+"/refresh", map[string]int64{"revision": 1}, cookies)
		if response.Code != test.status || !bytes.Contains(response.Body.Bytes(), []byte(test.code)) || bytes.Contains(response.Body.Bytes(), []byte("password")) {
			t.Fatalf("%s: %d %s", test.id, response.Code, response.Body.String())
		}
	}
	if feedCalled.Load() {
		t.Fatal("private source request reached its HTTP server")
	}
	if missing := mutationRequest(handler, http.MethodPost, "/api/v1/sources/missing/refresh", map[string]int64{"revision": 1}, cookies); missing.Code != http.StatusNotFound {
		t.Fatal(missing.Code, missing.Body.String())
	}
	if wrongPath := mutationRequest(handler, http.MethodPost, "/api/v1/sources/private/refresh/again", map[string]int64{"revision": 1}, cookies); wrongPath.Code != http.StatusNotFound {
		t.Fatal(wrongPath.Code, wrongPath.Body.String())
	}
}

func TestSourceRefreshFailuresKeepExistingInventory(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("https://user:password@should-not-leak.invalid:443"))
	}))
	defer feed.Close()
	handler, server, repository, _, audits, cookies := refreshTestServer(t)
	server.sourceResolver = loopbackResolver()
	server.sourcePolicy = security.DestinationPolicy{AllowTrusted: true}
	source := contract.Source("failing")
	source.Config["url"] = feed.URL
	if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
		t.Fatal(err)
	}
	existing := contract.Endpoint("preserved")
	if _, err := repository.Put(t.Context(), existing, 0); err != nil {
		t.Fatal(err)
	}
	response := mutationRequest(handler, http.MethodPost, "/api/v1/sources/failing/refresh", map[string]int64{"revision": 1}, cookies)
	if response.Code != http.StatusBadGateway || bytes.Contains(response.Body.Bytes(), []byte("password")) || bytes.Contains(response.Body.Bytes(), []byte(feed.URL)) {
		t.Fatal(response.Code, response.Body.String())
	}
	stored, err := repository.Get(t.Context(), existing.ID)
	if err != nil || stored.Revision != 1 || stored.Endpoint.SourceID != "" {
		t.Fatal(stored, err)
	}
	failed, err := repository.GetSource(t.Context(), source.ID)
	if err != nil || failed.Revision != 2 || failed.Source.LastRefreshStatus != "failed: fetch" || failed.Source.LastRefreshAt.IsZero() {
		t.Fatal(failed, err)
	}
	events, err := audits.ListAudit(t.Context(), audit.Page{Limit: 20})
	failureEvents := 0
	for _, event := range events {
		if event.Action == "source.refresh_failed" {
			failureEvents++
		}
	}
	if err != nil || failureEvents != 1 {
		t.Fatal(events, err)
	}
}

func TestSourceRefreshPreservesManagedFieldsAndUnlistedEndpoints(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("http://one.example.invalid:8080"))
	}))
	defer feed.Close()
	handler, server, repository, _, _, cookies := refreshTestServer(t)
	server.sourceResolver = loopbackResolver()
	server.sourcePolicy = security.DestinationPolicy{AllowTrusted: true}
	source := contract.Source("source")
	source.Config["url"] = feed.URL
	if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
		t.Fatal(err)
	}
	managed := contract.Endpoint("managed")
	managed.Protocol = proxy.HTTP
	managed.Host = "one.example.invalid"
	managed.Port = 8080
	managed.Name = "Operator label"
	managed.Tags = []string{"production"}
	managed.SourceID = "old-source"
	unlisted := contract.Endpoint("unlisted")
	for _, endpoint := range []proxy.Endpoint{managed, unlisted} {
		if _, err := repository.Put(t.Context(), endpoint, 0); err != nil {
			t.Fatal(err)
		}
	}

	response := mutationRequest(handler, http.MethodPost, "/api/v1/sources/source/refresh", map[string]int64{"revision": 1}, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"created":0`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"updated":1`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	stored, err := repository.Get(t.Context(), managed.ID)
	if err != nil || stored.Revision != 2 || stored.Endpoint.SourceID != source.ID || stored.Endpoint.Name != managed.Name || len(stored.Endpoint.Tags) != 1 || stored.Endpoint.Tags[0] != "production" {
		t.Fatal(stored, err)
	}
	if preserved, err := repository.Get(t.Context(), unlisted.ID); err != nil || preserved.Revision != 1 {
		t.Fatal(preserved, err)
	}
}

func TestSourceRefreshParseFailureDoesNotMutateEndpoints(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("bad\nstill-bad"))
	}))
	defer feed.Close()
	handler, server, repository, _, _, cookies := refreshTestServer(t)
	server.sourceResolver = loopbackResolver()
	server.sourcePolicy = security.DestinationPolicy{AllowTrusted: true}
	source := contract.Source("parse-failure")
	source.Config["url"] = feed.URL
	if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
		t.Fatal(err)
	}
	existing := contract.Endpoint("preserved")
	if _, err := repository.Put(t.Context(), existing, 0); err != nil {
		t.Fatal(err)
	}

	response := mutationRequest(handler, http.MethodPost, "/api/v1/sources/parse-failure/refresh", map[string]int64{"revision": 1}, cookies)
	if response.Code != http.StatusUnprocessableEntity || !bytes.Contains(response.Body.Bytes(), []byte("SOURCE_PARSE_FAILED")) {
		t.Fatal(response.Code, response.Body.String())
	}
	stored, err := repository.Get(t.Context(), existing.ID)
	if err != nil || stored.Revision != 1 {
		t.Fatal(stored, err)
	}
	failed, err := repository.GetSource(t.Context(), source.ID)
	if err != nil || failed.Revision != 2 || failed.Source.LastRefreshStatus != "failed: no valid endpoints" {
		t.Fatal(failed, err)
	}
}

func refreshTestServer(t *testing.T) (http.Handler, *Server, *sqlite.Store, *admin.Service, *audit.Memory, string) {
	t.Helper()
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	users := &memoryUsers{users: map[string]userRecord{}}
	service, err := admin.New(users, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	audits, err := audit.NewMemory(20)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(service, nil, repository, audits)
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.SetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	setup := request(server.Handler(), http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	if setup.Code != http.StatusCreated {
		t.Fatal(setup.Code, setup.Body.String())
	}
	return server.Handler(), server, repository, service, audits, cookiesFor(setup)
}

func loopbackResolver() sourceResolverFunc {
	return func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
}
