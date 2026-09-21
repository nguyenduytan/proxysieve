package api

import (
	"bytes"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalshadow "github.com/nguyenduytan/proxysieve/internal/shadow"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
)

func TestShadowLifecycleAndComparison(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "shadow-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	active := testRoutePolicy("active", "direct")
	if _, err = repository.PutPolicy(t.Context(), active, 0); err != nil {
		t.Fatal(err)
	}
	service, _ := admin.New(repository, security.DefaultPasswordParams())
	server, _ := New(service, nil, repository, repository, repository)
	manager, _ := internalshadow.New(nil)
	server.SetShadowControl(manager)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	adminCookies := cookiesFor(setup)
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)

	config := publicshadow.Config{ID: "candidate", Name: "Candidate", ActivePolicyID: active.ID, Policy: testRoutePolicy("candidate-policy", "block"), Enabled: true}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/shadow", map[string]any{"shadow": config}, viewerCookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	response := mutationRequest(handler, http.MethodPost, "/api/v1/shadow", map[string]any{"shadow": config}, adminCookies)
	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	manager.Observe(active.ID, policy.RequestContext{Host: "example.invalid"}, policy.Visibility{Host: true}, policy.Result{Actions: []policy.Action{{Type: "direct"}}}, nil)
	response = request(handler, http.MethodGet, "/api/v1/shadow/candidate/comparison", nil, viewerCookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"different_decisions":1`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/policies/active", map[string]any{"revision": 1}, adminCookies); response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("POLICY_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/shadow/candidate", map[string]any{"revision": 1}, adminCookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = request(handler, http.MethodGet, "/api/v1/shadow/candidate/comparison", nil, viewerCookies); response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
}

func testRoutePolicy(id, action string) policy.Policy {
	return policy.Policy{Version: 1, ID: model.ID(id), Name: id, Rules: []policy.Rule{{
		ID: "route", Name: "Route", Enabled: true, StopProcessing: true,
		Conditions: policy.Condition{}, Actions: []policy.Action{{Type: action}},
	}}}
}
