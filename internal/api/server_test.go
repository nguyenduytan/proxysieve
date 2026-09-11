package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/memory"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	publictraffic "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type memoryUsers struct {
	mu    sync.Mutex
	users map[string]userRecord
}
type userRecord struct {
	user auth.User
	hash string
}

func (m *memoryUsers) UserCount(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}
func (m *memoryUsers) CreateUser(_ context.Context, user auth.User, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[user.Username]; ok {
		return store.ErrConflict
	}
	m.users[user.Username] = userRecord{user, hash}
	return nil
}
func (m *memoryUsers) CreateInitialUser(ctx context.Context, user auth.User, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) != 0 {
		return store.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.users[user.Username] = userRecord{user, hash}
	return nil
}
func (m *memoryUsers) FindUser(_ context.Context, username string) (auth.User, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.users[username]
	if !ok {
		return auth.User{}, "", store.ErrNotFound
	}
	return record.user, record.hash, nil
}
func (m *memoryUsers) UpdateLastLogin(_ context.Context, id model.ID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for username, record := range m.users {
		if record.user.ID == id {
			record.user.LastLoginAt = at
			m.users[username] = record
			return nil
		}
	}
	return store.ErrNotFound
}

func TestSetupAndAuthenticatedAPI(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	adminService, err := admin.New(users, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	recorder, _ := internaltraffic.NewMemory(2)
	_ = recorder.Record(context.Background(), publictraffic.Event{Host: "example.invalid", Action: "block"})
	server, err := New(adminService, recorder, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	unauth := request(handler, http.MethodGet, "/api/v1/system/info", nil, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatal(unauth.Code, unauth.Body.String())
	}
	status := request(handler, http.MethodGet, "/api/v1/auth/setup-status", nil, "")
	if status.Code != http.StatusOK || !bytes.Contains(status.Body.Bytes(), []byte(`"setup_required":true`)) {
		t.Fatal(status.Code, status.Body.String())
	}
	token, err := adminService.SetupToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	if setup.Code != http.StatusCreated {
		t.Fatal(setup.Code, setup.Body.String())
	}
	cookies := cookiesFor(setup)
	me := request(handler, http.MethodGet, "/api/v1/auth/me", nil, cookies)
	if me.Code != http.StatusOK || !bytes.Contains(me.Body.Bytes(), []byte(`"username":"tony"`)) {
		t.Fatal(me.Code, me.Body.String())
	}
	traffic := request(handler, http.MethodGet, "/api/v1/traffic/live", nil, cookies)
	if traffic.Code != http.StatusOK || !bytes.Contains(traffic.Body.Bytes(), []byte(`"host":"example.invalid"`)) {
		t.Fatal(traffic.Code, traffic.Body.String())
	}
	logout := request(handler, http.MethodPost, "/api/v1/auth/logout", nil, cookies)
	if logout.Code != http.StatusForbidden {
		t.Fatal(logout.Code, logout.Body.String())
	}
	csrf := cookieValueFrom(cookies, csrfCookie)
	logout = requestWithCSRF(handler, cookies, csrf)
	if logout.Code != http.StatusNoContent {
		t.Fatal(logout.Code, logout.Body.String())
	}
	me = request(handler, http.MethodGet, "/api/v1/auth/me", nil, cookies)
	if me.Code != http.StatusUnauthorized {
		t.Fatal(me.Code, me.Body.String())
	}
}
func TestAPIValidationAndSecurityHeaders(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil)
	response := request(server.Handler(), http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "x", "password": "y", "unexpected": "z"}, "")
	if response.Code != http.StatusBadRequest || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal(response.Code, response.Header())
	}
	badHost := httptest.NewRequest(http.MethodGet, "/health", nil)
	badHost.Host = "example.com"
	writer := httptest.NewRecorder()
	server.Handler().ServeHTTP(writer, badHost)
	if writer.Code != http.StatusBadRequest {
		t.Fatal(writer.Code)
	}
}
func TestDashboardAssetsAreServedWithoutBypassingAPIAuth(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil)
	page := request(server.Handler(), http.MethodGet, "/", nil, "")
	if page.Code != http.StatusOK || !bytes.Contains(page.Body.Bytes(), []byte(`<div id="root"></div>`)) {
		t.Fatal(page.Code, page.Body.String())
	}
	api := request(server.Handler(), http.MethodGet, "/api/v1/auth/me", nil, "")
	if api.Code != http.StatusUnauthorized {
		t.Fatal(api.Code, api.Body.String())
	}
}
func TestProxyInventoryRequiresSessionAndPaginates(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	endpoints, err := memory.NewEndpoints(10)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := contract.Endpoint("proxy")
	endpoint.CredentialRef = secret.Ref("secret://upstream/auth")
	if _, err = endpoints.Put(context.Background(), endpoint, 0); err != nil {
		t.Fatal(err)
	}
	server, _ := New(service, nil, endpoints)
	handler := server.Handler()
	unauth := request(handler, http.MethodGet, "/api/v1/proxies", nil, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatal(unauth.Code)
	}
	token, _ := service.SetupToken(context.Background())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	response := request(handler, http.MethodGet, "/api/v1/proxies?limit=1", nil, cookiesFor(setup))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"id":"proxy"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"credential_ref":"secret://upstream/auth"`)) || bytes.Contains(response.Body.Bytes(), []byte("fake-password")) {
		t.Fatal(response.Code, response.Body.String())
	}
	invalid := request(handler, http.MethodGet, "/api/v1/proxies?limit=1001", nil, cookiesFor(setup))
	if invalid.Code != http.StatusBadRequest {
		t.Fatal(invalid.Code, invalid.Body.String())
	}
}
func TestOperatorCreatesProxyWithCSRF(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	endpoints, _ := memory.NewEndpoints(10)
	server, _ := New(service, nil, endpoints)
	handler := server.Handler()
	token, _ := service.SetupToken(context.Background())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	input := map[string]any{"endpoint": map[string]any{"id": "proxy", "name": "Proxy", "protocol": "http", "host": "proxy.example.invalid", "port": 8080, "enabled": true, "credential_ref": "secret://upstream/auth"}}
	missingCSRF := request(handler, http.MethodPost, "/api/v1/proxies", input, cookies)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatal(missingCSRF.Code, missingCSRF.Body.String())
	}
	encoded, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxies", bytes.NewReader(encoded))
	req.Host = "127.0.0.1"
	req.Header.Set("Cookie", cookies)
	req.Header.Set("X-CSRF-Token", cookieValueFrom(cookies, csrfCookie))
	req.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, req)
	if writer.Code != http.StatusCreated || !bytes.Contains(writer.Body.Bytes(), []byte(`"runtime_active":false`)) {
		t.Fatal(writer.Code, writer.Body.String())
	}
	stored, err := endpoints.Get(context.Background(), "proxy")
	if err != nil || stored.Endpoint.CredentialRef != "secret://upstream/auth" {
		t.Fatal(stored, err)
	}
}
func TestProxyImportPreviewRequiresOperatorCSRF(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil)
	handler := server.Handler()
	token, _ := service.SetupToken(context.Background())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	payload, _ := json.Marshal(map[string]string{"input": "proxy.example.invalid:8080\nbad"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxies/import/preview", bytes.NewReader(payload))
	req.Host = "127.0.0.1"
	req.Header.Set("Cookie", cookies)
	req.Header.Set("X-CSRF-Token", cookieValueFrom(cookies, csrfCookie))
	req.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, req)
	if writer.Code != http.StatusOK || !bytes.Contains(writer.Body.Bytes(), []byte(`"valid":1`)) || !bytes.Contains(writer.Body.Bytes(), []byte(`"invalid":1`)) {
		t.Fatal(writer.Code, writer.Body.String())
	}
}
func request(handler http.Handler, method, path string, body any, cookies string) *httptest.ResponseRecorder {
	var input *bytes.Reader
	if body == nil {
		input = bytes.NewReader(nil)
	} else {
		encoded, _ := json.Marshal(body)
		input = bytes.NewReader(encoded)
	}
	r := httptest.NewRequest(method, path, input)
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	if cookies != "" {
		r.Header.Set("Cookie", cookies)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func requestWithCSRF(handler http.Handler, cookies, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.Host = "127.0.0.1"
	r.Header.Set("Cookie", cookies)
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func cookiesFor(w *httptest.ResponseRecorder) string {
	cookies := w.Result().Cookies()
	return cookies[0].Name + "=" + cookies[0].Value + "; " + cookies[1].Name + "=" + cookies[1].Value
}
func cookieValueFrom(raw, name string) string {
	for _, part := range bytes.Split([]byte(raw), []byte("; ")) {
		pair := bytes.SplitN(part, []byte("="), 2)
		if len(pair) == 2 && string(pair[0]) == name {
			return string(pair[1])
		}
	}
	return ""
}
