package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type fakeRuntimeControl struct {
	current store.RuntimeRecord
	history []store.RuntimeRecord
	staged  bool
}

func (f *fakeRuntimeControl) CurrentRuntime() store.RuntimeRecord { return f.current }
func (f *fakeRuntimeControl) HasStagedRuntimeChanges(context.Context) (bool, error) {
	return f.staged, nil
}
func (f *fakeRuntimeControl) ListRuntime(context.Context, int64, int) ([]store.RuntimeRecord, error) {
	return f.history, nil
}
func (f *fakeRuntimeControl) ActivateRuntime(_ context.Context, expected int64, actor model.ID) (store.RuntimeRecord, error) {
	if expected != f.current.Revision {
		return store.RuntimeRecord{}, store.ErrConflict
	}
	f.current.Revision++
	f.current.ActivatedAt = time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)
	f.current.ActivatedBy = actor
	f.staged = false
	f.history = append([]store.RuntimeRecord{f.current}, f.history...)
	return f.current, nil
}
func (f *fakeRuntimeControl) RollbackRuntime(_ context.Context, expected, target int64, actor model.ID) (store.RuntimeRecord, error) {
	if expected != f.current.Revision {
		return store.RuntimeRecord{}, store.ErrConflict
	}
	if target != 1 {
		return store.RuntimeRecord{}, store.ErrNotFound
	}
	f.current.Revision++
	f.current.SourceRevision = target
	f.current.ActivatedAt = time.Date(2026, 9, 14, 3, 1, 0, 0, time.UTC)
	f.current.ActivatedBy = actor
	f.staged = true
	return f.current, nil
}

func TestRuntimeEndpointsEnforceRolesCSRFAndRevisions(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	audits, _ := audit.NewMemory(20)
	server, _ := New(service, nil, nil, audits)
	control := &fakeRuntimeControl{current: store.RuntimeRecord{Revision: 1, Bundle: store.RuntimeBundle{}}, history: []store.RuntimeRecord{{Revision: 1, ActivatedAt: time.Now().UTC(), ActivatedBy: "admin"}}, staged: true}
	server.SetRuntimeControl(control)
	handler := server.Handler()
	if response := request(handler, http.MethodGet, "/api/v1/runtime", nil, ""); response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code, response.Body.String())
	}
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)
	if response := request(handler, http.MethodGet, "/api/v1/runtime", nil, viewerCookies); response.Code != http.StatusOK || !containsAll(response.Body.String(), `"revision":1`, `"source":"inventory"`, `"policy_count":0`, `"staged_changes":true`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := request(handler, http.MethodGet, "/api/v1/runtime/history?limit=20", nil, viewerCookies); response.Code != http.StatusOK || !containsAll(response.Body.String(), `"items"`, `"revision":1`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/runtime/activate", map[string]any{"expected_revision": 1}, viewerCookies); response.Code != http.StatusForbidden {
		t.Fatal("viewer activated runtime", response.Code, response.Body.String())
	}
	operatorToken, _ := service.CreateSession(auth.User{ID: "operator", Username: "operator", Role: auth.RoleOperator, Enabled: true, CreatedAt: time.Now().UTC()})
	operatorCookies := sessionCookie + "=" + operatorToken + "; " + csrfCookie + "=" + service.CSRFToken(operatorToken)
	if response := request(handler, http.MethodPost, "/api/v1/runtime/activate", map[string]any{"expected_revision": 1}, operatorCookies); response.Code != http.StatusForbidden {
		t.Fatal("activation without CSRF succeeded", response.Code, response.Body.String())
	}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/runtime/activate", map[string]any{"expected_revision": 0}, operatorCookies); response.Code != http.StatusConflict {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/runtime/activate", map[string]any{"expected_revision": 1}, operatorCookies); response.Code != http.StatusOK || !containsAll(response.Body.String(), `"revision":2`, `"activated_by":"operator"`, `"staged_changes":false`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/runtime/rollback", map[string]any{"expected_revision": 2, "target_revision": 99}, operatorCookies); response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/runtime/rollback", map[string]any{"expected_revision": 2, "target_revision": 1}, operatorCookies); response.Code != http.StatusOK || !containsAll(response.Body.String(), `"revision":3`, `"source":"rollback"`, `"source_revision":1`, `"staged_changes":true`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if events, err := audits.ListAudit(t.Context(), audit.Page{Limit: 20}); err != nil || !hasAuditActions(events, "runtime.activated", "runtime.rolled_back") {
		t.Fatal(events, err)
	}
}

func TestRuntimeItemRepresentationsDistinguishActiveAndStagedInventory(t *testing.T) {
	endpoint := contract.Endpoint("proxy")
	pool := routing.Pool{ID: "pool", Name: "Pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, Enabled: true}
	document := policy.Policy{Version: 1, ID: "default", Name: "Default", Rules: []policy.Rule{{ID: "reject", Name: "Reject", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "reject"}}}}}
	server := &Server{runtimeControl: &fakeRuntimeControl{current: store.RuntimeRecord{Revision: 7, Bundle: store.RuntimeBundle{Proxies: []proxy.Endpoint{endpoint}, Pools: []routing.Pool{pool}, Policies: []policy.Policy{document}}}}}

	activeProxy := server.proxyResponse(store.EndpointRecord{Endpoint: endpoint, Revision: 3})
	activePool := server.poolResponse(store.PoolRecord{Pool: pool, Revision: 4})
	activePolicy := server.policyResponse(store.PolicyRecord{Policy: document, Revision: 5})
	for name, response := range map[string]map[string]any{"proxy": activeProxy, "pool": activePool, "policy": activePolicy} {
		if response["runtime_active"] != true || response["activation"] != "active" || response["runtime_revision"] != int64(7) {
			t.Fatalf("%s active response: %#v", name, response)
		}
	}

	endpoint.Name = "Staged proxy"
	pool.Name = "Staged pool"
	document.Name = "Staged policy"
	for name, response := range map[string]map[string]any{
		"proxy":  server.proxyResponse(store.EndpointRecord{Endpoint: endpoint, Revision: 4}),
		"pool":   server.poolResponse(store.PoolRecord{Pool: pool, Revision: 5}),
		"policy": server.policyResponse(store.PolicyRecord{Policy: document, Revision: 6}),
	} {
		if response["runtime_active"] != false || response["activation"] != "staged" || response["runtime_revision"] != int64(7) {
			t.Fatalf("%s staged response: %#v", name, response)
		}
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

var _ RuntimeControl = (*fakeRuntimeControl)(nil)
