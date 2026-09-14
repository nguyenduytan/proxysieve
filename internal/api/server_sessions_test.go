package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsession "github.com/nguyenduytan/proxysieve/internal/session"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

type sessionAudit struct{ events []audit.Event }

func (w *sessionAudit) Record(_ context.Context, event audit.Event) error {
	w.events = append(w.events, event)
	return nil
}

func TestSessionAPIReadRotateDeleteAndAudit(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, err := admin.New(users, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := internalsession.New([]byte("a session api test key that is long enough"), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Resolve(t.Context(), internalsession.Request{
		ClientID: "client", PoolID: "pool", Key: "raw-private-key",
		Policy: publicsession.Policy{Strategy: publicsession.Explicit, TTL: time.Hour},
		Select: func(context.Context) (model.ID, error) { return "proxy", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	audits := &sessionAudit{}
	server, err := New(service, nil, nil, audits)
	if err != nil {
		t.Fatal(err)
	}
	server.SetSessions(manager)
	handler := server.Handler()
	setupToken, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": setupToken, "username": "admin", "password": "a sufficient fake admin password"}, "")
	adminCookies := cookiesFor(setup)
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)

	response := request(handler, http.MethodGet, "/api/v1/sessions", nil, viewerCookies)
	if response.Code != http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte("raw-private-key")) || !bytes.Contains(response.Body.Bytes(), []byte(created.Session.KeyHash)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = request(handler, http.MethodGet, "/api/v1/sessions/"+string(created.Session.ID), nil, viewerCookies)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/sessions/"+string(created.Session.ID)+"/rotate", map[string]any{}, viewerCookies)
	if response.Code != http.StatusForbidden {
		t.Fatal("viewer rotated session", response.Code)
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/sessions/"+string(created.Session.ID)+"/rotate", map[string]any{}, adminCookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"rotated"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"rotation_reason":"manual"`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/sessions/"+string(created.Session.ID), map[string]any{}, adminCookies)
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	response = request(handler, http.MethodGet, "/api/v1/sessions/"+string(created.Session.ID), nil, viewerCookies)
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
	if !hasAuditActions(audits.events, "session.rotated", "session.deleted") {
		t.Fatal(audits.events)
	}
}
