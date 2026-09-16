package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

func TestBudgetStatuses(t *testing.T) {
	manager, err := internalbudget.New([]publicbudget.Config{{ID: "system", Name: "System", Limit: 100, Hard: true, Action: publicbudget.ActionReject}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{budgets: manager}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/budgets", nil)
	response := httptest.NewRecorder()
	server.listBudgets(response, request)
	var body struct {
		Items []internalbudget.Status `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Items) != 1 || body.Items[0].ID != "system" || body.Items[0].Remaining != 100 {
		t.Fatal(response.Code, response.Body.String())
	}

}

func TestBudgetStatusesAreEmptyWithoutConfiguration(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).listBudgets(response, httptest.NewRequest(http.MethodGet, "/api/v1/budgets", nil))
	if response.Code != http.StatusOK || response.Body.String() != "{\"items\":[]}\n" {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestBudgetLifecycleIsRevisionedAndProtectsScopeReferences(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "budget-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	for _, id := range []string{"proxy", "solo-proxy"} {
		if _, err = repository.Put(t.Context(), contract.Endpoint(id), 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = repository.PutPool(t.Context(), contract.Pool("paid-pool"), 0); err != nil {
		t.Fatal(err)
	}
	client := auth.Client{ID: "paid-client", Name: "Paid client", Enabled: true, AuthMethod: "api_key", CreatedAt: time.Now().UTC()}
	if _, err = repository.PutClient(t.Context(), client, 0); err != nil {
		t.Fatal(err)
	}
	manager, err := internalbudget.NewPersistent(nil, 100, repository)
	if err != nil {
		t.Fatal(err)
	}
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, repository, repository, repository)
	server.SetBudgets(manager)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	adminCookies := cookiesFor(setup)
	operatorToken, _ := service.CreateSession(auth.User{ID: "operator", Username: "operator", Role: auth.RoleOperator, Enabled: true, CreatedAt: time.Now().UTC()})
	operatorCookies := sessionCookie + "=" + operatorToken + "; " + csrfCookie + "=" + service.CSRFToken(operatorToken)
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)

	system := publicbudget.Config{ID: "system", Name: "System", Scope: publicbudget.ScopeSystem, Limit: 100, Hard: true, Action: publicbudget.ActionReject, Window: publicbudget.WindowLifetime}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/budgets", map[string]any{"budget": system}, viewerCookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	response := mutationRequest(handler, http.MethodPost, "/api/v1/budgets", map[string]any{"budget": system}, operatorCookies)
	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = request(handler, http.MethodGet, "/api/v1/budgets/system/usage", nil, viewerCookies); response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"remaining_bytes":100`)) {
		t.Fatal(response.Code, response.Body.String())
	}

	missing := system
	missing.ID, missing.Name, missing.Scope, missing.ScopeID = "missing", "Missing", publicbudget.ScopePool, "missing"
	if response = mutationRequest(handler, http.MethodPost, "/api/v1/budgets", map[string]any{"budget": missing}, operatorCookies); response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("BUDGET_POOL_NOT_FOUND")) {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, configured := range []publicbudget.Config{
		{ID: "pool-budget", Name: "Pool", Scope: publicbudget.ScopePool, ScopeID: "paid-pool", Limit: 100, Hard: true, Action: publicbudget.ActionReject},
		{ID: "client-budget", Name: "Client", Scope: publicbudget.ScopeClient, ScopeID: client.ID, Limit: 100, Hard: true, Action: publicbudget.ActionReject},
		{ID: "proxy-budget", Name: "Proxy", Scope: publicbudget.ScopeProxy, ScopeID: "solo-proxy", Limit: 100, Hard: true, Action: publicbudget.ActionReject},
	} {
		if response = mutationRequest(handler, http.MethodPost, "/api/v1/budgets", map[string]any{"budget": configured}, operatorCookies); response.Code != http.StatusCreated {
			t.Fatal(configured.ID, response.Code, response.Body.String())
		}
	}
	for _, deletion := range []struct {
		path string
		body map[string]any
		code string
	}{
		{"/api/v1/pools/paid-pool", map[string]any{"revision": 1}, "POOL_IN_USE"},
		{"/api/v1/proxies/solo-proxy", map[string]any{"revision": 1}, "PROXY_IN_USE"},
		{"/api/v1/clients/paid-client", map[string]any{"revision": 1}, "CLIENT_IN_USE"},
	} {
		if response = mutationRequest(handler, http.MethodDelete, deletion.path, deletion.body, adminCookies); response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte(deletion.code)) {
			t.Fatal(deletion.path, response.Code, response.Body.String())
		}
	}

	system.Limit = 50
	response = mutationRequest(handler, http.MethodPatch, "/api/v1/budgets/system", map[string]any{"budget": system, "revision": 1}, operatorCookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodPatch, "/api/v1/budgets/system", map[string]any{"budget": system, "revision": 1}, operatorCookies); response.Code != http.StatusConflict {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/budgets/system", map[string]any{"revision": 2}, operatorCookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = request(handler, http.MethodGet, "/api/v1/budgets/system", nil, viewerCookies); response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
	if got := manager.ApplicableIDs(model.ID("paid-client"), "paid-pool", "solo-proxy"); len(got) != 3 {
		t.Fatal(got)
	}
	events, err := repository.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil || !hasAuditActions(events, "budget.created", "budget.updated", "budget.deleted") {
		t.Fatal(events, err)
	}
}
