package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
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
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"github.com/nguyenduytan/proxysieve/pkg/session"
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
	clients map[string]store.ClientRecord
	keys    map[string]auth.APIKey
}
type memoryControlStore struct {
	*memory.Endpoints
	*memory.Sources
}

func (m *memoryClients) GetClient(_ context.Context, id model.ID) (store.ClientRecord, error) {
	record, ok := m.clients[string(id)]
	if !ok {
		return store.ClientRecord{}, store.ErrNotFound
	}
	record.Client = record.Client.Clone()
	return record, nil
}
func (m *memoryClients) PutClient(_ context.Context, client auth.Client, expected int64) (store.ClientRecord, error) {
	record, exists := m.clients[string(client.ID)]
	if expected == 0 {
		if exists {
			return store.ClientRecord{}, store.ErrConflict
		}
		record = store.ClientRecord{Client: client.Clone(), Revision: 1}
		m.clients[string(client.ID)] = record
		return record, nil
	}
	if !exists {
		return store.ClientRecord{}, store.ErrNotFound
	}
	if record.Revision != expected {
		return store.ClientRecord{}, store.ErrConflict
	}
	record = store.ClientRecord{Client: client.Clone(), Revision: expected + 1}
	m.clients[string(client.ID)] = record
	return record, nil
}
func (m *memoryClients) DeleteClient(_ context.Context, id model.ID, expected int64) error {
	record, ok := m.clients[string(id)]
	if !ok {
		return store.ErrNotFound
	}
	if record.Revision != expected {
		return store.ErrConflict
	}
	delete(m.clients, string(id))
	for keyID, key := range m.keys {
		if key.ClientID == id {
			delete(m.keys, keyID)
		}
	}
	return nil
}
func (m *memoryClients) ListClients(_ context.Context, page store.Page) ([]store.ClientRecord, error) {
	if err := page.Validate(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(m.clients))
	for id := range m.clients {
		if id > string(page.After) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > page.Limit {
		ids = ids[:page.Limit]
	}
	records := make([]store.ClientRecord, 0, len(ids))
	for _, id := range ids {
		record := m.clients[id]
		record.Client = record.Client.Clone()
		records = append(records, record)
	}
	return records, nil
}
func (m *memoryClients) CreateAPIKey(_ context.Context, key auth.APIKey, _ [32]byte) (auth.APIKey, error) {
	if _, ok := m.clients[string(key.ClientID)]; !ok {
		return auth.APIKey{}, store.ErrNotFound
	}
	m.keys[string(key.ID)] = key
	return key, nil
}
func (m *memoryClients) ListAPIKeys(_ context.Context, clientID model.ID, limit int) ([]auth.APIKey, error) {
	items := make([]auth.APIKey, 0, limit)
	for _, key := range m.keys {
		if key.ClientID == clientID {
			items = append(items, key)
		}
	}
	return items, nil
}
func (m *memoryClients) RevokeAPIKey(_ context.Context, clientID, keyID model.ID, at time.Time) error {
	key, ok := m.keys[string(keyID)]
	if !ok || key.ClientID != clientID || key.RevokedAt != nil {
		return store.ErrNotFound
	}
	key.RevokedAt = &at
	m.keys[string(keyID)] = key
	return nil
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

func TestProxyImportPreviewAcceptsMappedJSON(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	response := mutationRequest(handler, http.MethodPost, "/api/v1/proxies/import/preview", map[string]any{
		"input":   `{"rows":[{"address":"proxy.example.invalid","listen":8080}]}`,
		"format":  "json",
		"mapping": map[string]string{"items_field": "rows", "host_field": "address", "port_field": "listen"},
	}, cookiesFor(setup))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"valid":1`)) {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestProxyImportPreviewAllowsBoundedPayloadAboveDefaultAPILimit(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	input := strings.Repeat("http://large.example.invalid:8080\n", 2_000)
	response := mutationRequest(handler, http.MethodPost, "/api/v1/proxies/import/preview", map[string]any{"input": input}, cookiesFor(setup))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"valid":2000`)) {
		t.Fatal(response.Code, response.Body.String())
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
	for _, id := range []model.ID{"first", "second"} {
		if err := audits.Record(t.Context(), audit.Event{ID: id, At: time.Unix(10, 0).UTC(), Action: "proxy.created", TargetType: "proxy"}); err != nil {
			t.Fatal(err)
		}
	}
	var page struct {
		Items        []audit.Event `json:"items"`
		NextBefore   string        `json:"next_before"`
		NextBeforeID string        `json:"next_before_id"`
	}
	result = request(handler, http.MethodGet, "/api/v1/audit?limit=1", nil, cookiesFor(setup))
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.NextBefore == "" || page.NextBeforeID == "" {
		t.Fatal(result.Code, result.Body.String())
	}
	result = request(handler, http.MethodGet, "/api/v1/audit?limit=1&before="+page.NextBefore+"&before_id="+page.NextBeforeID, nil, cookiesFor(setup))
	if result.Code != http.StatusOK || !bytes.Contains(result.Body.Bytes(), []byte(`"id":"second"`)) {
		t.Fatal(result.Code, result.Body.String())
	}
	if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.NextBefore == "" || page.NextBeforeID == "" {
		t.Fatal(result.Code, result.Body.String())
	}
	result = request(handler, http.MethodGet, "/api/v1/audit?limit=1&before="+page.NextBefore+"&before_id="+page.NextBeforeID, nil, cookiesFor(setup))
	if result.Code != http.StatusOK || !bytes.Contains(result.Body.Bytes(), []byte(`"id":"first"`)) {
		t.Fatal(result.Code, result.Body.String())
	}
}

func TestAuditTrailWithoutReaderReturnsStablePageShape(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, nil, nil)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	result := request(handler, http.MethodGet, "/api/v1/audit", nil, cookiesFor(setup))
	var page struct {
		Items        []audit.Event `json:"items"`
		NextBefore   string        `json:"next_before"`
		NextBeforeID string        `json:"next_before_id"`
	}
	if result.Code != http.StatusOK || json.Unmarshal(result.Body.Bytes(), &page) != nil || len(page.Items) != 0 || page.NextBefore != "" || page.NextBeforeID != "" {
		t.Fatal(result.Code, result.Body.String())
	}
}

func TestPoolLifecycleValidatesReferencesCyclesAndRevisions(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), t.TempDir()+"/pools.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if _, err = repository.Put(t.Context(), contract.Endpoint("proxy"), 0); err != nil {
		t.Fatal(err)
	}
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, repository, repository, repository)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)
	if response := request(handler, http.MethodGet, "/api/v1/pools", nil, viewerCookies); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/pools", map[string]any{"pool": routing.Pool{}}, viewerCookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}

	missing := routing.Pool{ID: "missing", Name: "Missing endpoint", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"does-not-exist"}, Enabled: true}
	response := mutationRequest(handler, http.MethodPost, "/api/v1/pools", map[string]any{"pool": missing}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("POOL_ENDPOINT_NOT_FOUND")) {
		t.Fatal(response.Code, response.Body.String())
	}

	primary := routing.Pool{ID: "primary", Name: "Primary", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, SessionPolicy: session.Policy{Strategy: session.Client, TTL: time.Hour, IdleTTL: time.Minute, MaxRequests: 100, MaxBytes: 1 << 20}, Enabled: true}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/pools", map[string]any{"pool": primary}, cookies)
	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":1`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"runtime_active":false`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"session_policy":{"strategy":"client","ttl_ns":3600000000000,"idle_ttl_ns":60000000000,"max_requests":100,"max_bytes":1048576}`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	primary.Name = "Updated primary"
	response = mutationRequest(handler, http.MethodPatch, "/api/v1/pools/primary", map[string]any{"pool": primary, "revision": 1}, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	missingFallback := routing.Pool{ID: "missing-fallback", Name: "Missing fallback", Strategy: routing.Random, FallbackPoolIDs: []model.ID{"unknown"}, Enabled: true}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/pools", map[string]any{"pool": missingFallback}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("POOL_FALLBACK_NOT_FOUND")) {
		t.Fatal(response.Code, response.Body.String())
	}
	fallback := routing.Pool{ID: "fallback", Name: "Fallback", Strategy: routing.Random, FallbackPoolIDs: []model.ID{"primary"}, Enabled: true}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/pools", map[string]any{"pool": fallback}, cookies)
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body.String())
	}

	primary.FallbackPoolIDs = []model.ID{"fallback"}
	response = mutationRequest(handler, http.MethodPatch, "/api/v1/pools/primary", map[string]any{"pool": primary, "revision": 2}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("POOL_FALLBACK_CYCLE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/pools/primary", map[string]any{"revision": 2}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("POOL_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = request(handler, http.MethodGet, "/api/v1/pools?limit=1", nil, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"next_after":"fallback"`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/proxies/proxy", map[string]any{"revision": 1}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("PROXY_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/pools/fallback", map[string]any{"revision": 1}, cookies)
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/pools/primary", map[string]any{"revision": 2}, cookies)
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	audits, err := repository.ListAudit(t.Context(), audit.Page{Limit: 20})
	if err != nil || !hasAuditActions(audits, "pool.created", "pool.updated", "pool.deleted") {
		t.Fatal(audits, err)
	}
}

func TestChainLifecycleValidatesReferencesOverlapAndPolicyUse(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), t.TempDir()+"/chains.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	firstEndpoint := contract.Endpoint("first-proxy")
	secondEndpoint := contract.Endpoint("second-proxy")
	secondEndpoint.Host = "second.example.invalid"
	for _, endpoint := range []proxy.Endpoint{firstEndpoint, secondEndpoint} {
		if _, err = repository.Put(t.Context(), endpoint, 0); err != nil {
			t.Fatal(err)
		}
	}
	firstPool := routing.Pool{ID: "first-pool", Name: "First", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"first-proxy"}, Enabled: true}
	secondPool := routing.Pool{ID: "second-pool", Name: "Second", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"second-proxy"}, Enabled: true}
	for _, pool := range []routing.Pool{firstPool, secondPool} {
		if _, err = repository.PutPool(t.Context(), pool, 0); err != nil {
			t.Fatal(err)
		}
	}
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, repository, repository, repository)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)
	viewerToken, _ := service.CreateSession(auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: time.Now().UTC()})
	viewerCookies := sessionCookie + "=" + viewerToken + "; " + csrfCookie + "=" + service.CSRFToken(viewerToken)

	chain := routing.Chain{ID: "ordered-chain", Name: "Ordered chain", Hops: []routing.Hop{{PoolID: "first-pool", Timeout: time.Second}, {PoolID: "second-pool", Timeout: 2 * time.Second}}, Enabled: true}
	if response := mutationRequest(handler, http.MethodPost, "/api/v1/chains", map[string]any{"chain": chain}, viewerCookies); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	missing := chain.Clone()
	missing.ID = "missing"
	missing.Hops[1].PoolID = "unknown"
	response := mutationRequest(handler, http.MethodPost, "/api/v1/chains", map[string]any{"chain": missing}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("CHAIN_POOL_NOT_FOUND")) {
		t.Fatal(response.Code, response.Body.String())
	}
	overlapPool := routing.Pool{ID: "overlap-pool", Name: "Overlap", Strategy: routing.Random, EndpointIDs: []model.ID{"first-proxy"}, Enabled: true}
	if _, err = repository.PutPool(t.Context(), overlapPool, 0); err != nil {
		t.Fatal(err)
	}
	overlap := chain.Clone()
	overlap.ID = "overlap"
	overlap.Hops[1].PoolID = "overlap-pool"
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains", map[string]any{"chain": overlap}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("CHAIN_ENDPOINT_OVERLAP")) {
		t.Fatal(response.Code, response.Body.String())
	}
	firstPool.SessionPolicy = session.Policy{Strategy: session.Client}
	if _, err = repository.PutPool(t.Context(), firstPool, 1); err != nil {
		t.Fatal(err)
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains", map[string]any{"chain": chain}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("CHAIN_SESSION_UNSUPPORTED")) {
		t.Fatal(response.Code, response.Body.String())
	}
	firstPool.SessionPolicy = session.Policy{}
	if _, err = repository.PutPool(t.Context(), firstPool, 2); err != nil {
		t.Fatal(err)
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains", map[string]any{"chain": chain}, cookies)
	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":1`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"runtime_active":false`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	chain.Name = "Updated ordered chain"
	response = mutationRequest(handler, http.MethodPatch, "/api/v1/chains/ordered-chain", map[string]any{"chain": chain, "revision": 1}, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = request(handler, http.MethodGet, "/api/v1/chains?limit=1", nil, viewerCookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"id":"ordered-chain"`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains/ordered-chain/test", map[string]any{"target_host": "example.com", "target_port": 443}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("CHAIN_NOT_ACTIVE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	server.SetRuntimeControl(&fakeRuntimeControl{current: store.RuntimeRecord{Revision: 1, Bundle: store.RuntimeBundle{Chains: []routing.Chain{chain}}}})
	server.SetChainTester(func(_ context.Context, id model.ID, host string, port uint16) ChainTestResult {
		if id != chain.ID || host != "example.com" || port != 443 {
			t.Fatalf("unexpected chain test target: %s %s:%d", id, host, port)
		}
		return ChainTestResult{Status: "healthy", Latency: int64(5 * time.Millisecond)}
	})
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains/ordered-chain/test", map[string]any{"target_host": "example.com", "target_port": 443}, viewerCookies)
	if response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains/ordered-chain/test", map[string]any{"target_host": "bad host", "target_port": 443}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("INVALID_CHAIN_TARGET")) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/chains/ordered-chain/test", map[string]any{"target_host": "example.com", "target_port": 443}, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"healthy"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"latency_ns":5000000`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = request(handler, http.MethodGet, "/api/v1/chains/ordered-chain", nil, viewerCookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"health":{"status":"healthy"`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	alternative := chain
	alternative.ID = "alternative-chain"
	alternative.Name = "Alternative chain"
	alternative.Hops = []routing.Hop{chain.Hops[1], chain.Hops[0]}
	if _, err = repository.PutChain(t.Context(), alternative, 0); err != nil {
		t.Fatal(err)
	}
	document := policy.Policy{Version: 1, ID: "chain-policy", Name: "Chain policy", Rules: []policy.Rule{{ID: "chain", Name: "Chain", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "chain", ChainID: chain.ID, FallbackChainIDs: []model.ID{alternative.ID}}}}}}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/policies", map[string]any{"policy": document}, cookies)
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/chains/ordered-chain", map[string]any{"revision": 2}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("CHAIN_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/chains/alternative-chain", map[string]any{"revision": 1}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("CHAIN_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/pools/first-pool", map[string]any{"revision": 3}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("POOL_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/policies/chain-policy", map[string]any{"revision": 1}, cookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/chains/alternative-chain", map[string]any{"revision": 1}, cookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = mutationRequest(handler, http.MethodDelete, "/api/v1/chains/ordered-chain", map[string]any{"revision": 2}, cookies); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	if events, err := repository.ListAudit(t.Context(), audit.Page{Limit: 30}); err != nil || !hasAuditActions(events, "chain.created", "chain.updated", "chain.tested", "chain.deleted") {
		t.Fatal(events, err)
	}
}

func hasAuditActions(events []audit.Event, actions ...string) bool {
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.Action] = true
	}
	for _, action := range actions {
		if !seen[action] {
			return false
		}
	}
	return true
}

func TestPolicyLifecycleAndSimulation(t *testing.T) {
	repository, err := sqlite.Open(t.Context(), t.TempDir()+"/policies.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if _, err = repository.Put(t.Context(), contract.Endpoint("proxy"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.PutPool(t.Context(), routing.Pool{ID: "pool", Name: "Pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, Enabled: true}, 0); err != nil {
		t.Fatal(err)
	}
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	server, _ := New(service, nil, repository, repository, repository)
	handler := server.Handler()
	token, _ := service.SetupToken(t.Context())
	setup := request(handler, http.MethodPost, "/api/v1/auth/setup", map[string]string{"token": token, "username": "tony", "password": "a sufficient fake admin password"}, "")
	cookies := cookiesFor(setup)

	missing := policy.Policy{Version: 1, ID: "missing", Name: "Missing pool", Rules: []policy.Rule{{ID: "route", Name: "Route", Enabled: true, StopProcessing: true, Actions: []policy.Action{{Type: "proxy", PoolID: "unknown"}}}}}
	response := mutationRequest(handler, http.MethodPost, "/api/v1/policies", map[string]any{"policy": missing}, cookies)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("POLICY_POOL_NOT_FOUND")) {
		t.Fatal(response.Code, response.Body.String())
	}
	document := policy.Policy{Version: 1, ID: "default", Name: "Default", Rules: []policy.Rule{{ID: "proxy", Name: "Proxy example hosts", Priority: 10, Enabled: true, StopProcessing: true, Conditions: policy.Condition{Field: "host", Operator: "suffix", Values: []string{"example.invalid"}}, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/policies", map[string]any{"policy": document}, cookies)
	if response.Code != http.StatusCreated || !bytes.Contains(response.Body.Bytes(), []byte(`"runtime_active":false`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	var simulation map[string]any
	response = mutationRequest(handler, http.MethodPost, "/api/v1/policies/default/simulate", map[string]any{"listener": "http", "protocol": "http", "host": "api.example.invalid", "port": 443, "method": "GET", "path": "/v1"}, cookies)
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &simulation) != nil || simulation["outcome"] != "proxy" || simulation["simulation_only"] != true {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodPost, "/api/v1/policies/default/simulate", map[string]any{"listener": "http", "protocol": "http", "host": "other.invalid", "port": 443}, cookies)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"outcome":"no_route"`)) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = mutationRequest(handler, http.MethodDelete, "/api/v1/pools/pool", map[string]any{"revision": 1}, cookies)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("POOL_IN_USE")) {
		t.Fatal(response.Code, response.Body.String())
	}
	if events, err := repository.ListAudit(t.Context(), audit.Page{Limit: 20}); err != nil || !hasAuditActions(events, "policy.created") {
		t.Fatal(events, err)
	}
}

func TestPolicySimulationReportsFirstTerminalAction(t *testing.T) {
	record := store.PolicyRecord{Policy: policy.Policy{ID: "default"}, Revision: 4}
	result := policy.Result{Actions: []policy.Action{
		{Type: "cache"},
		{Type: "rewrite", Value: "/small"},
		{Type: "proxy", PoolID: "pool"},
		{Type: "reject"},
	}}
	response := representPolicySimulation(record, result)
	if response.Outcome != "proxy" {
		t.Fatalf("simulation outcome=%q, want first terminal action proxy", response.Outcome)
	}
}

func TestClientAndAPIKeyAreCreatedWithoutPersistingToken(t *testing.T) {
	users := &memoryUsers{users: map[string]userRecord{}}
	service, _ := admin.New(users, security.DefaultPasswordParams())
	clients := &memoryClients{clients: map[string]store.ClientRecord{}, keys: map[string]auth.APIKey{}}
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
	if !bytes.Contains(createdClient.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(createdClient.Body.String())
	}
	operatorToken, _ := service.CreateSession(auth.User{ID: "operator", Username: "operator", Role: auth.RoleOperator, Enabled: true, CreatedAt: time.Now().UTC()})
	operatorCookies := sessionCookie + "=" + operatorToken + "; " + csrfCookie + "=" + service.CSRFToken(operatorToken)
	if forbidden := request(handler, http.MethodGet, "/api/v1/clients", nil, operatorCookies); forbidden.Code != http.StatusForbidden {
		t.Fatal("operator listed clients", forbidden.Code, forbidden.Body.String())
	}
	gotClient := request(handler, http.MethodGet, "/api/v1/clients/"+string(client.ID), nil, cookies)
	if gotClient.Code != http.StatusOK || !bytes.Contains(gotClient.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatal(gotClient.Code, gotClient.Body.String())
	}
	client.Name = "Updated durable client"
	client.Enabled = false
	updatedClient := mutationRequest(handler, http.MethodPatch, "/api/v1/clients/"+string(client.ID), map[string]any{"client": client, "revision": 1}, cookies)
	if updatedClient.Code != http.StatusOK || !bytes.Contains(updatedClient.Body.Bytes(), []byte(`"revision":2`)) || !bytes.Contains(updatedClient.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatal(updatedClient.Code, updatedClient.Body.String())
	}
	staleClient := mutationRequest(handler, http.MethodPatch, "/api/v1/clients/"+string(client.ID), map[string]any{"client": client, "revision": 1}, cookies)
	if staleClient.Code != http.StatusConflict || !bytes.Contains(staleClient.Body.Bytes(), []byte("CLIENT_CONFLICT")) {
		t.Fatal(staleClient.Code, staleClient.Body.String())
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
	if staleDelete := mutationRequest(handler, http.MethodDelete, "/api/v1/clients/"+string(client.ID), map[string]any{"revision": 1}, cookies); staleDelete.Code != http.StatusConflict {
		t.Fatal(staleDelete.Code, staleDelete.Body.String())
	}
	if deleted := mutationRequest(handler, http.MethodDelete, "/api/v1/clients/"+string(client.ID), map[string]any{"revision": 2}, cookies); deleted.Code != http.StatusNoContent {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
	if missing := request(handler, http.MethodGet, "/api/v1/clients/"+string(client.ID), nil, cookies); missing.Code != http.StatusNotFound {
		t.Fatal(missing.Code, missing.Body.String())
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
	event := publictraffic.Event{At: base.Add(5 * time.Minute), RequestID: "request", ConnectionID: "connection", ClientID: "client-a", PolicyID: "policy-a", RuleID: "rule-a", PoolID: "pool-a", Host: "example.invalid", Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 8}
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
	breakdown := request(server.Handler(), http.MethodGet, "/api/v1/traffic/breakdown"+query+"&dimension=pool&policy_id=policy-a&rule_id=rule-a&limit=5", nil, cookies)
	if breakdown.Code != http.StatusOK || !bytes.Contains(breakdown.Body.Bytes(), []byte(`"dimension":"pool"`)) || !bytes.Contains(breakdown.Body.Bytes(), []byte(`"value":"pool-a"`)) || !bytes.Contains(breakdown.Body.Bytes(), []byte(`"upstream_download_bytes":8`)) {
		t.Fatal(breakdown.Code, breakdown.Body.String())
	}
	for _, path := range []string{"/api/v1/traffic/breakdown?dimension=host", "/api/v1/traffic/breakdown?dimension=pool&limit=101"} {
		invalidBreakdown := request(server.Handler(), http.MethodGet, path, nil, cookies)
		if invalidBreakdown.Code != http.StatusBadRequest {
			t.Fatal(path, invalidBreakdown.Code, invalidBreakdown.Body.String())
		}
	}
	if unauthenticatedBreakdown := request(server.Handler(), http.MethodGet, "/api/v1/traffic/breakdown?dimension=pool", nil, ""); unauthenticatedBreakdown.Code != http.StatusUnauthorized {
		t.Fatal(unauthenticatedBreakdown.Code, unauthenticatedBreakdown.Body.String())
	}
	if postBreakdown := request(server.Handler(), http.MethodPost, "/api/v1/traffic/breakdown?dimension=pool", nil, cookies); postBreakdown.Code != http.StatusMethodNotAllowed {
		t.Fatal(postBreakdown.Code, postBreakdown.Body.String())
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
