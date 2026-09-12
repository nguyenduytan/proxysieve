package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/keys"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/internal/storage/memory"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
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
type memoryClients struct {
	clients map[string]auth.Client
	keys    map[string]auth.APIKey
}
type memoryControlStore struct {
	*memory.Endpoints
	*memory.Sources
}

func (m *memoryClients) CreateClient(_ context.Context, client auth.Client) error {
	if _, ok := m.clients[string(client.ID)]; ok {
		return store.ErrConflict
	}
	m.clients[string(client.ID)] = client
	return nil
}
func (m *memoryClients) CreateAPIKey(_ context.Context, key auth.APIKey, _ [32]byte) (auth.APIKey, error) {
	if _, ok := m.clients[string(key.ClientID)]; !ok {
		return auth.APIKey{}, store.ErrNotFound
	}
	m.keys[string(key.ID)] = key
	return key, nil
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
	server, err := New(adminService, recorder, nil, nil)
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
	if traffic.Code != http.StatusOK || !bytes.Contains(traffic.Body.Bytes(), []byte(`"host":"example.invalid"`)) || !bytes.Contains(traffic.Body.Bytes(), []byte(`"durable":`)) {
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
	server, _ := New(service, nil, nil, nil)
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
	server, _ := New(service, nil, nil, nil)
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
	server, _ := New(service, nil, endpoints, nil)
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
	server, _ := New(service, nil, endpoints, nil)
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

func TestProxyLifecycleUsesOptimisticRevision(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	endpoints, _ := memory.NewEndpoints(10)
	endpoint := contract.Endpoint("proxy")
	if _, err := endpoints.Put(t.Context(), endpoint, 0); err != nil {
		t.Fatal(err)
	}
	server, _ := New(service, nil, endpoints, nil)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)

	got := request(handler, http.MethodGet, "/api/v1/proxies/proxy", nil, cookies)
	if got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(got.Code, got.Body.String())
	}
	updated := endpoint
	updated.Name = "Updated proxy"
	patch := mutationRequest(handler, http.MethodPatch, "/api/v1/proxies/proxy", map[string]any{"endpoint": updated, "revision": 1}, cookies)
	if patch.Code != http.StatusOK || !bytes.Contains(patch.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatal(patch.Code, patch.Body.String())
	}
	stale := mutationRequest(handler, http.MethodPatch, "/api/v1/proxies/proxy", map[string]any{"endpoint": updated, "revision": 1}, cookies)
	if stale.Code != http.StatusConflict || !bytes.Contains(stale.Body.Bytes(), []byte(`"PROXY_CONFLICT"`)) {
		t.Fatal(stale.Code, stale.Body.String())
	}
	staleDelete := mutationRequest(handler, http.MethodDelete, "/api/v1/proxies/proxy", map[string]any{"revision": 1}, cookies)
	if staleDelete.Code != http.StatusConflict {
		t.Fatal(staleDelete.Code, staleDelete.Body.String())
	}
	deleted := mutationRequest(handler, http.MethodDelete, "/api/v1/proxies/proxy", map[string]any{"revision": 2}, cookies)
	if deleted.Code != http.StatusNoContent {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
	missing := request(handler, http.MethodGet, "/api/v1/proxies/proxy", nil, cookies)
	if missing.Code != http.StatusNotFound {
		t.Fatal(missing.Code, missing.Body.String())
	}
}

func TestSourceLifecycleUsesRBACCSRFAndOptimisticRevision(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	endpoints, _ := memory.NewEndpoints(10)
	sources, _ := memory.NewSources(10)
	controlStore := &memoryControlStore{Endpoints: endpoints, Sources: sources}
	audits, _ := audit.NewMemory(20)
	server, _ := New(service, nil, controlStore, audits)
	handler := server.Handler()

	if response := request(handler, http.MethodGet, "/api/v1/sources", nil, ""); response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code, response.Body.String())
	}
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	source := contract.Source("source")
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)
	viewerMutation := mutationRequest(handler, http.MethodPost, "/api/v1/sources", map[string]any{"source": source}, viewerCookies)
	if viewerMutation.Code != http.StatusForbidden {
		t.Fatal(viewerMutation.Code, viewerMutation.Body.String())
	}

	missingCSRF := request(handler, http.MethodPost, "/api/v1/sources", map[string]any{"source": source}, cookies)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatal(missingCSRF.Code, missingCSRF.Body.String())
	}
	unsafe := source.Clone()
	unsafe.ID = "unsafe"
	unsafe.Config["api_token"] = "must-not-be-stored"
	unsafeResponse := mutationRequest(handler, http.MethodPost, "/api/v1/sources", map[string]any{"source": unsafe}, cookies)
	if unsafeResponse.Code != http.StatusBadRequest {
		t.Fatal(unsafeResponse.Code, unsafeResponse.Body.String())
	}
	created := mutationRequest(handler, http.MethodPost, "/api/v1/sources", map[string]any{"source": source}, cookies)
	if created.Code != http.StatusCreated || !bytes.Contains(created.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(created.Code, created.Body.String())
	}
	listed := request(handler, http.MethodGet, "/api/v1/sources?limit=1", nil, cookies)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"id":"source"`)) {
		t.Fatal(listed.Code, listed.Body.String())
	}
	got := request(handler, http.MethodGet, "/api/v1/sources/source", nil, cookies)
	if got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(`"url":"https://source.example.invalid/list"`)) {
		t.Fatal(got.Code, got.Body.String())
	}

	source.Name = "Updated source"
	source.LastRefreshAt = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	source.LastRefreshStatus = "forged"
	updated := mutationRequest(handler, http.MethodPatch, "/api/v1/sources/source", map[string]any{"source": source, "revision": 1}, cookies)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"revision":2`)) || bytes.Contains(updated.Body.Bytes(), []byte("forged")) {
		t.Fatal(updated.Code, updated.Body.String())
	}
	stale := mutationRequest(handler, http.MethodPatch, "/api/v1/sources/source", map[string]any{"source": source, "revision": 1}, cookies)
	if stale.Code != http.StatusConflict || !bytes.Contains(stale.Body.Bytes(), []byte("SOURCE_CONFLICT")) {
		t.Fatal(stale.Code, stale.Body.String())
	}
	staleDelete := mutationRequest(handler, http.MethodDelete, "/api/v1/sources/source", map[string]any{"revision": 1}, cookies)
	if staleDelete.Code != http.StatusConflict {
		t.Fatal(staleDelete.Code, staleDelete.Body.String())
	}
	deleted := mutationRequest(handler, http.MethodDelete, "/api/v1/sources/source", map[string]any{"revision": 2}, cookies)
	if deleted.Code != http.StatusNoContent {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
	events, err := audits.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil {
		t.Fatal(events, err)
	}
	sourceEvents := 0
	for _, event := range events {
		if event.TargetType == "source" {
			sourceEvents++
		}
	}
	if sourceEvents != 3 {
		t.Fatal(events)
	}
}

func TestProxyImportPreviewRequiresOperatorCSRF(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
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

func TestProxyImportCommitsAtomicallyWithDuplicateModes(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	endpoints, _ := memory.NewEndpoints(10)
	existing := contract.Endpoint("existing")
	existing.Host = "existing.example.invalid"
	if _, err := endpoints.Put(t.Context(), existing, 0); err != nil {
		t.Fatal(err)
	}
	server, _ := New(service, nil, endpoints, nil)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)

	input := "http://existing.example.invalid:8080\nhttp://new.example.invalid:8080\nbad"
	created := mutationRequest(handler, http.MethodPost, "/api/v1/proxies/import", map[string]any{"input": input}, cookies)
	if created.Code != http.StatusCreated || !bytes.Contains(created.Body.Bytes(), []byte(`"created":1`)) || !bytes.Contains(created.Body.Bytes(), []byte(`"skipped":1`)) {
		t.Fatal(created.Code, created.Body.String())
	}
	rows, err := endpoints.List(t.Context(), store.Page{Limit: 10})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	updated := mutationRequest(handler, http.MethodPost, "/api/v1/proxies/import", map[string]any{"input": "http://existing.example.invalid:8080", "mode": proxy.UpdateDuplicates}, cookies)
	if updated.Code != http.StatusCreated || !bytes.Contains(updated.Body.Bytes(), []byte(`"created":0`)) || !bytes.Contains(updated.Body.Bytes(), []byte(`"updated":1`)) {
		t.Fatal(updated.Code, updated.Body.String())
	}
	stored, err := endpoints.Get(t.Context(), existing.ID)
	if err != nil || stored.Endpoint.Port != 8080 || stored.Revision != 2 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestAuditTrailIsAdminOnlyAndSanitized(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	audits, _ := audit.NewMemory(10)
	server, _ := New(service, nil, nil, audits)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	result := request(handler, http.MethodGet, "/api/v1/audit", nil, cookiesFor(setup))
	if result.Code != http.StatusOK || !bytes.Contains(result.Body.Bytes(), []byte(`"action":"admin.setup"`)) || bytes.Contains(result.Body.Bytes(), []byte("fake admin password")) {
		t.Fatal(result.Code, result.Body.String())
	}
}
func TestClientAndAPIKeyAreCreatedWithoutPersistingToken(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	clients := &memoryClients{clients: map[string]auth.Client{}, keys: map[string]auth.APIKey{}}
	server, _ := New(service, nil, nil, nil, clients)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	clientResponse := mutationRequest(handler, http.MethodPost, "/api/v1/clients", map[string]string{"name": "Browser automation"}, cookies)
	if clientResponse.Code != 201 {
		t.Fatal(clientResponse.Code, clientResponse.Body.String())
	}
	var client auth.Client
	if err := json.Unmarshal(clientResponse.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	keyResponse := mutationRequest(handler, http.MethodPost, "/api/v1/clients/"+string(client.ID)+"/api-keys", map[string]string{}, cookies)
	if keyResponse.Code != 201 {
		t.Fatal(keyResponse.Code, keyResponse.Body.String())
	}
	var value struct {
		APIKey auth.APIKey `json:"api_key"`
		Token  string      `json:"token"`
	}
	if err := json.Unmarshal(keyResponse.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if !keys.Valid(value.Token) || value.APIKey.Prefix != value.Token[:12] || strings.Contains(marshal(clients.keys), value.Token) {
		t.Fatal(value)
	}
}

func TestClientAndAPIKeyLifecycleUsesDurableStore(t *testing.T) {
	durable, err := sqlite.Open(t.Context(), t.TempDir()+"/clients.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	audits, _ := audit.NewMemory(20)
	server, _ := New(service, nil, nil, audits, durable)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)

	createdClient := mutationRequest(handler, http.MethodPost, "/api/v1/clients", map[string]string{"name": "Durable client"}, cookies)
	if createdClient.Code != http.StatusCreated {
		t.Fatal(createdClient.Code, createdClient.Body.String())
	}
	var client auth.Client
	if err := json.Unmarshal(createdClient.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	createdKey := mutationRequest(handler, http.MethodPost, "/api/v1/clients/"+string(client.ID)+"/api-keys", map[string]string{}, cookies)
	if createdKey.Code != http.StatusCreated {
		t.Fatal(createdKey.Code, createdKey.Body.String())
	}
	var keyResponse struct {
		APIKey auth.APIKey `json:"api_key"`
		Token  string      `json:"token"`
	}
	if err := json.Unmarshal(createdKey.Body.Bytes(), &keyResponse); err != nil {
		t.Fatal(err)
	}
	if !keys.Valid(keyResponse.Token) {
		t.Fatal("generated API token was invalid")
	}

	listedClients := request(handler, http.MethodGet, "/api/v1/clients", nil, cookies)
	if listedClients.Code != http.StatusOK || !bytes.Contains(listedClients.Body.Bytes(), []byte(string(client.ID))) || bytes.Contains(listedClients.Body.Bytes(), []byte(keyResponse.Token)) {
		t.Fatal(listedClients.Code, listedClients.Body.String())
	}
	listedKeys := request(handler, http.MethodGet, "/api/v1/clients/"+string(client.ID)+"/api-keys", nil, cookies)
	if listedKeys.Code != http.StatusOK || !bytes.Contains(listedKeys.Body.Bytes(), []byte(string(keyResponse.APIKey.ID))) || bytes.Contains(listedKeys.Body.Bytes(), []byte(keyResponse.Token)) {
		t.Fatal(listedKeys.Code, listedKeys.Body.String())
	}

	wrongClient := mutationRequest(handler, http.MethodDelete, "/api/v1/clients/wrong-client/api-keys/"+string(keyResponse.APIKey.ID), nil, cookies)
	if wrongClient.Code != http.StatusNotFound {
		t.Fatal("key revoked through mismatched client path", wrongClient.Code, wrongClient.Body.String())
	}
	revoked := mutationRequest(handler, http.MethodDelete, "/api/v1/clients/"+string(client.ID)+"/api-keys/"+string(keyResponse.APIKey.ID), nil, cookies)
	if revoked.Code != http.StatusNoContent {
		t.Fatal(revoked.Code, revoked.Body.String())
	}
	listedKeys = request(handler, http.MethodGet, "/api/v1/clients/"+string(client.ID)+"/api-keys", nil, cookies)
	if listedKeys.Code != http.StatusOK || !bytes.Contains(listedKeys.Body.Bytes(), []byte("revoked_at")) {
		t.Fatal(listedKeys.Code, listedKeys.Body.String())
	}
	auditResponse := request(handler, http.MethodGet, "/api/v1/audit", nil, cookies)
	if auditResponse.Code != http.StatusOK || !bytes.Contains(auditResponse.Body.Bytes(), []byte("api_key.revoked")) {
		t.Fatal(auditResponse.Code, auditResponse.Body.String())
	}
	if method := request(handler, http.MethodPatch, "/api/v1/clients", nil, cookies); method.Code != http.StatusMethodNotAllowed {
		t.Fatal("PATCH clients", method.Code, method.Body.String())
	}
}

func TestTrafficHistoryUsesDurableStoreWhenAvailable(t *testing.T) {
	durable, err := sqlite.Open(t.Context(), t.TempDir()+"/traffic.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, durable, nil)
	event := publictraffic.Event{At: time.Now().UTC(), RequestID: "request", ConnectionID: "connection", Host: "example.invalid", Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 8}
	if err = durable.RecordTraffic(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	token, _ := service.SetupToken(t.Context())
	setup := request(server.Handler(), http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	response := request(server.Handler(), http.MethodGet, "/api/v1/traffic/history", nil, cookiesFor(setup))
	if response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(`"host":"example.invalid"`)) {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestTrafficAnalyticsAreBoundedAndAuthenticated(t *testing.T) {
	durable, err := sqlite.Open(t.Context(), t.TempDir()+"/analytics.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, nil, durable, nil)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return base.Add(30 * time.Minute) }
	event := publictraffic.Event{At: base.Add(5 * time.Minute), RequestID: "request", ConnectionID: "connection", ClientID: "client-a", Host: "example.invalid", Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 8}
	if err = durable.RecordTraffic(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	token, _ := service.SetupToken(t.Context())
	setup := request(server.Handler(), http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	unauthenticated := request(server.Handler(), http.MethodGet, "/api/v1/traffic/summary", nil, "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatal(unauthenticated.Code, unauthenticated.Body.String())
	}
	query := "?from=2026-09-01T00:00:00Z&until=2026-09-01T01:00:00Z&client_id=client-a"
	summary := request(server.Handler(), http.MethodGet, "/api/v1/traffic/summary"+query, nil, cookies)
	if summary.Code != http.StatusOK || !bytes.Contains(summary.Body.Bytes(), []byte(`"request_count":1`)) || !bytes.Contains(summary.Body.Bytes(), []byte(`"upstream_download_bytes":8`)) {
		t.Fatal(summary.Code, summary.Body.String())
	}
	series := request(server.Handler(), http.MethodGet, "/api/v1/traffic/timeseries"+query+"&granularity=hour", nil, cookies)
	if series.Code != http.StatusOK || !bytes.Contains(series.Body.Bytes(), []byte(`"granularity":"hour"`)) || !bytes.Contains(series.Body.Bytes(), []byte(`"bucket_start":"2026-09-01T00:00:00Z"`)) {
		t.Fatal(series.Code, series.Body.String())
	}
	invalid := request(server.Handler(), http.MethodGet, "/api/v1/traffic/timeseries?granularity=week", nil, cookies)
	if invalid.Code != http.StatusBadRequest {
		t.Fatal(invalid.Code, invalid.Body.String())
	}
	wrongMethod := request(server.Handler(), http.MethodPost, "/api/v1/traffic/summary", nil, cookies)
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatal(wrongMethod.Code, wrongMethod.Body.String())
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
func mutationRequest(handler http.Handler, method, path string, body any, cookies string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(payload))
	r.Host = "127.0.0.1"
	r.Header.Set("Cookie", cookies)
	r.Header.Set("X-CSRF-Token", cookieValueFrom(cookies, csrfCookie))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func marshal(value any) string { body, _ := json.Marshal(value); return string(body) }
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
