package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type staticHealth struct{}

func (staticHealth) ProxyHealth(context.Context) ([]ProxyHealth, error) {
	return []ProxyHealth{{ProxyID: "proxy", Name: "Proxy", State: publichealth.Healthy, Circuit: publichealth.CircuitClosed, Score: 75}}, nil
}
func (staticHealth) PoolHealth(context.Context) ([]PoolHealth, error) {
	return []PoolHealth{{PoolID: "pool", Name: "Pool", Enabled: true, Total: 1, Eligible: 1, Healthy: 1}}, nil
}
func (staticHealth) CheckProxy(context.Context, model.ID, string, uint16) (ProxyHealth, error) {
	return ProxyHealth{}, nil
}
func (staticHealth) CheckPool(context.Context, model.ID, string, uint16) (PoolHealth, error) {
	return PoolHealth{}, nil
}

func TestHealthCollections(t *testing.T) {
	server := &Server{health: staticHealth{}}
	for _, test := range []struct {
		path string
		call func(http.ResponseWriter, *http.Request)
	}{{"/api/v1/health/proxies", server.proxyHealth}, {"/api/v1/health/pools", server.poolHealth}} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		test.call(response, request)
		var body struct {
			Items []json.RawMessage `json:"items"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Items) != 1 {
			t.Fatal(test.path, response.Code, response.Body.String())
		}
	}
}

type recordingHealth struct {
	staticHealth
	err  error
	id   model.ID
	host string
	port uint16
}

func (h *recordingHealth) CheckProxy(_ context.Context, id model.ID, host string, port uint16) (ProxyHealth, error) {
	h.id, h.host, h.port = id, host, port
	return ProxyHealth{ProxyID: id, Name: "Proxy"}, h.err
}

func (h *recordingHealth) CheckPool(_ context.Context, id model.ID, host string, port uint16) (PoolHealth, error) {
	h.id, h.host, h.port = id, host, port
	return PoolHealth{PoolID: id, Name: "Pool"}, h.err
}

func TestHealthChecksEnforceMutationSecurityAndValidation(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, err := admin.New(users, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	control := &recordingHealth{}
	server, err := New(service, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.SetHealth(control)
	handler := server.Handler()
	now := time.Now().UTC()
	operatorToken, _ := service.CreateSession(auth.User{ID: "operator", Username: "operator", Role: auth.RoleOperator, Enabled: true, CreatedAt: now})
	operatorCookies := sessionCookie + "=" + operatorToken + "; " + csrfCookie + "=" + service.CSRFToken(operatorToken)
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: now})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)
	body := map[string]any{"target_host": "example.com", "target_port": 443}

	checks := []struct {
		name     string
		response func() *httptest.ResponseRecorder
		status   int
	}{
		{"method", func() *httptest.ResponseRecorder {
			return request(handler, http.MethodGet, "/api/v1/health/proxies/proxy/check", nil, operatorCookies)
		}, http.StatusMethodNotAllowed},
		{"csrf", func() *httptest.ResponseRecorder {
			return request(handler, http.MethodPost, "/api/v1/health/proxies/proxy/check", body, operatorCookies)
		}, http.StatusForbidden},
		{"role", func() *httptest.ResponseRecorder {
			return mutationRequest(handler, http.MethodPost, "/api/v1/health/proxies/proxy/check", body, viewerCookies)
		}, http.StatusForbidden},
		{"id", func() *httptest.ResponseRecorder {
			return mutationRequest(handler, http.MethodPost, "/api/v1/health/proxies/bad!/check", body, operatorCookies)
		}, http.StatusBadRequest},
		{"host", func() *httptest.ResponseRecorder {
			return mutationRequest(handler, http.MethodPost, "/api/v1/health/proxies/proxy/check", map[string]any{"target_host": "", "target_port": 443}, operatorCookies)
		}, http.StatusBadRequest},
		{"port", func() *httptest.ResponseRecorder {
			return mutationRequest(handler, http.MethodPost, "/api/v1/health/proxies/proxy/check", map[string]any{"target_host": "example.com", "target_port": 0}, operatorCookies)
		}, http.StatusBadRequest},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			response := check.response()
			if response.Code != check.status {
				t.Fatal(response.Code, response.Body.String())
			}
		})
	}

	response := mutationRequest(handler, http.MethodPost, "/api/v1/health/proxies/proxy/check", body, operatorCookies)
	if response.Code != http.StatusOK || control.id != "proxy" || control.host != "example.com" || control.port != 443 {
		t.Fatal(response.Code, response.Body.String(), control.id, control.host, control.port)
	}
	for checkErr, status := range map[error]int{store.ErrNotFound: http.StatusNotFound, ErrHealthBusy: http.StatusConflict, ErrHealthTargetDenied: http.StatusForbidden} {
		control.err = checkErr
		response = mutationRequest(handler, http.MethodPost, "/api/v1/health/pools/pool/check", body, operatorCookies)
		if response.Code != status {
			t.Fatal(checkErr, response.Code, response.Body.String())
		}
	}
}
