package api

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	internalalert "github.com/nguyenduytan/proxysieve/internal/alert"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	publicalert "github.com/nguyenduytan/proxysieve/pkg/alert"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type testAlertControl struct{ store store.Alerts }

func (c testAlertControl) Stats() internalalert.Stats { return internalalert.Stats{Succeeded: 1} }
func (c testAlertControl) Test(ctx context.Context, id model.ID) (publicalert.Delivery, error) {
	if _, err := c.store.GetWebhook(ctx, id); err != nil {
		return publicalert.Delivery{}, err
	}
	delivery := publicalert.Delivery{ID: model.NewID(), WebhookID: id, EventID: model.NewID(), AttemptedAt: time.Now().UTC(), Attempt: 1, Success: true, StatusCode: http.StatusNoContent}
	return delivery, c.store.RecordWebhookDelivery(ctx, delivery)
}

func TestAlertAndWebhookLifecycle(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "alerts-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	service, _ := admin.New(repository, security.DefaultPasswordParams())
	server, _ := New(service, nil, repository, repository, repository)
	server.SetAlertControl(testAlertControl{store: repository})
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	adminCookies := cookiesFor(setup)
	operatorToken, _ := service.CreateSession(auth.User{ID: "operator", Username: "operator", Role: auth.RoleOperator, Enabled: true, CreatedAt: time.Now().UTC()})
	operatorCookies := sessionCookie + "=" + operatorToken + "; " + csrfCookie + "=" + service.CSRFToken(operatorToken)

	if response := request(handler, http.MethodGet, "/api/v1/webhooks", nil, operatorCookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	invalid := publicalert.Webhook{ID: "primary", Name: "Primary", URL: "http://127.0.0.1/hook", Enabled: true}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/webhooks", map[string]any{"webhook": invalid}, adminCookies); response.Code != http.StatusBadRequest {
		t.Fatal(response.Code, response.Body.String())
	}
	webhook := publicalert.Webhook{ID: "primary", Name: "Primary", URL: "https://alerts.example/hook", SecretRef: "secret://webhooks/primary", Enabled: true}
	response := mutationRequest(handler, http.MethodPost, "/api/v1/webhooks", map[string]any{"webhook": webhook}, adminCookies)
	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	missing := publicalert.Rule{ID: "missing", Name: "Missing", EventTypes: []string{"source.refresh_failed"}, WebhookIDs: []model.ID{"unknown"}, Enabled: true}
	if response = mutationRequest(handler, http.MethodPost, "/api/v1/alerts", map[string]any{"rule": missing}, adminCookies); response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("ALERT_WEBHOOK_NOT_FOUND")) {
		t.Fatal(response.Code, response.Body.String())
	}
	rule := publicalert.Rule{ID: "failures", Name: "Failures", EventTypes: []string{"source.refresh_failed"}, WebhookIDs: []model.ID{webhook.ID}, Enabled: true}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/alerts", map[string]any{"rule": rule}, adminCookies)
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/webhooks/primary", map[string]any{"revision": 1}, adminCookies); response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("WEBHOOK_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodPost, "/api/v1/webhooks/primary/test", nil, adminCookies); response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"success":true`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = request(handler, http.MethodGet, "/api/v1/webhooks/primary/test", nil, adminCookies); response.Code != http.StatusMethodNotAllowed {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = request(handler, http.MethodGet, "/api/v1/webhooks/primary/deliveries?limit=10", nil, adminCookies); response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status_code":204`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodPost, "/api/v1/webhooks/primary/deliveries", nil, adminCookies); response.Code != http.StatusMethodNotAllowed {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/alerts/failures", map[string]any{"revision": 1}, adminCookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/webhooks/primary", map[string]any{"revision": 1}, adminCookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	events, err := repository.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil || !hasAuditActions(events, "webhook.created", "alert.created", "webhook.tested", "alert.deleted", "webhook.deleted") {
		t.Fatal(events, err)
	}
}
