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
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
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
	server, err := New(adminService, recorder)
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
	server, _ := New(service, nil)
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
