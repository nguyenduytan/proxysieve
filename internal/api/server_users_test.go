package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
)

func TestUserManagementRBACLockoutAndSessionInvalidation(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	service, err := admin.New(repository, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(service, nil, nil, repository)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := service.SetupToken(t.Context())
	setup := request(server.Handler(), http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "admin", "password": "a sufficient fake admin password"}, "")
	adminCookies := cookiesFor(setup)
	var current auth.User
	if err = json.Unmarshal(setup.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}

	created := mutationRequest(server.Handler(), http.MethodPost, "/api/v1/users", map[string]any{"username": "operator", "password": "a sufficient fake operator password", "role": "operator", "enabled": true}, adminCookies)
	if created.Code != http.StatusCreated || bytes.Contains(created.Body.Bytes(), []byte("password")) {
		t.Fatal(created.Code, created.Body.String())
	}
	var operator auth.User
	if err = json.Unmarshal(created.Body.Bytes(), &operator); err != nil {
		t.Fatal(err)
	}
	duplicate := mutationRequest(server.Handler(), http.MethodPost, "/api/v1/users", map[string]any{"username": "OPERATOR", "password": "a sufficient fake operator password", "role": "viewer", "enabled": true}, adminCookies)
	if duplicate.Code != http.StatusConflict {
		t.Fatal(duplicate.Code, duplicate.Body.String())
	}
	login := request(server.Handler(), http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "operator", "password": "a sufficient fake operator password"}, "")
	operatorCookies := cookiesFor(login)
	if forbidden := request(server.Handler(), http.MethodGet, "/api/v1/users", nil, operatorCookies); forbidden.Code != http.StatusForbidden {
		t.Fatal("operator listed users", forbidden.Code, forbidden.Body.String())
	}
	listed := request(server.Handler(), http.MethodGet, "/api/v1/users", nil, adminCookies)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"username":"operator"`)) {
		t.Fatal(listed.Code, listed.Body.String())
	}
	updated := mutationRequest(server.Handler(), http.MethodPatch, "/api/v1/users/"+string(operator.ID), map[string]any{"username": "operator", "password": "", "role": "viewer", "enabled": false}, adminCookies)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatal(updated.Code, updated.Body.String())
	}
	if staleSession := request(server.Handler(), http.MethodGet, "/api/v1/auth/me", nil, operatorCookies); staleSession.Code != http.StatusUnauthorized {
		t.Fatal("updated user session retained stale role", staleSession.Code)
	}
	if lockout := mutationRequest(server.Handler(), http.MethodPatch, "/api/v1/users/"+string(current.ID), map[string]any{"username": "admin", "password": "", "role": "viewer", "enabled": true}, adminCookies); lockout.Code != http.StatusConflict {
		t.Fatal("last admin demotion succeeded", lockout.Code, lockout.Body.String())
	}
	if selfDelete := mutationRequest(server.Handler(), http.MethodDelete, "/api/v1/users/"+string(current.ID), nil, adminCookies); selfDelete.Code != http.StatusConflict {
		t.Fatal("self delete succeeded", selfDelete.Code, selfDelete.Body.String())
	}
	if deleted := mutationRequest(server.Handler(), http.MethodDelete, "/api/v1/users/"+string(operator.ID), nil, adminCookies); deleted.Code != http.StatusNoContent {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
	events, err := repository.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil || !hasAuditActions(events, "user.created", "user.updated", "user.deleted") {
		t.Fatal(events, err)
	}
}
