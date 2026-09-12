// Package api provides the versioned local control-plane HTTP boundary.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
	"github.com/nguyenduytan/proxysieve/internal/keys"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	publictraffic "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

const sessionCookie = "proxysieve_session"
const csrfCookie = "proxysieve_csrf"
const maxBodyBytes = 64 << 10

//go:embed openapi.yaml
var openAPISpec []byte

type Server struct {
	admin         *admin.Service
	traffic       *internaltraffic.Memory
	endpoints     store.Endpoints
	clients       ClientStore
	trafficStore  TrafficStore
	trafficStatus TrafficStatus
	audit         audit.Writer
	now           func() time.Time
	ui            http.Handler
	limitMu       sync.Mutex
	windowStart   time.Time
	authAttempts  int
}

type ClientStore interface {
	CreateClient(context.Context, auth.Client) error
	CreateAPIKey(context.Context, auth.APIKey, [32]byte) (auth.APIKey, error)
}

type ClientReader interface {
	ListClients(context.Context, int) ([]auth.Client, error)
	ListAPIKeys(context.Context, model.ID, int) ([]auth.APIKey, error)
}

type APIKeyRevoker interface {
	RevokeAPIKey(context.Context, model.ID, model.ID, time.Time) error
}
type TrafficStore interface {
	ListTraffic(context.Context, sqlite.TrafficPage) ([]publictraffic.Event, error)
	TrafficSummary(context.Context, sqlite.TrafficQuery) (publictraffic.Summary, error)
	TrafficTimeseries(context.Context, sqlite.TrafficQuery) (publictraffic.Series, error)
}
type TrafficStatus interface {
	Stats() internaltraffic.AsyncStats
}

func New(service *admin.Service, traffic *internaltraffic.Memory, endpoints store.Endpoints, auditWriter audit.Writer, clientStores ...ClientStore) (*Server, error) {
	if service == nil {
		return nil, errors.New("admin service is required")
	}
	var clients ClientStore
	if len(clientStores) > 0 {
		clients = clientStores[0]
	}
	var durable TrafficStore
	if source, ok := endpoints.(TrafficStore); ok {
		durable = source
	}
	return &Server{admin: service, traffic: traffic, endpoints: endpoints, clients: clients, trafficStore: durable, audit: auditWriter, ui: dashboardHandler(), now: func() time.Time { return time.Now().UTC() }}, nil
}
func (s *Server) Handler() http.Handler                 { return securityHeaders(http.HandlerFunc(s.handle)) }
func (s *Server) SetTrafficStatus(status TrafficStatus) { s.trafficStatus = status }
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if !safeHost(r.Host) {
		writeError(w, http.StatusBadRequest, "INVALID_HOST", "The request host is not allowed.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "ORIGIN_REJECTED", "Cross-origin requests are not allowed.")
		return
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/setup") && !s.allowAuthAttempt() {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "AUTH_RATE_LIMIT", "Too many attempts. Try again in one minute.")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/health" && r.URL.Path != "/ready" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.ui.ServeHTTP(w, r)
		return
	}
	switch r.URL.Path {
	case "/health":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/ready":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	case "/api/v1/openapi.yaml":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openAPISpec)
	case "/api/v1/system/info":
		s.require(w, r, auth.RoleViewer, func(user auth.User) {
			writeJSON(w, http.StatusOK, map[string]any{"build": buildinfo.Current(), "user": user})
		})
	case "/api/v1/auth/setup-status":
		s.setupStatus(w, r)
	case "/api/v1/auth/setup":
		s.setup(w, r)
	case "/api/v1/auth/login":
		s.login(w, r)
	case "/api/v1/auth/logout":
		s.requireMutation(w, r, auth.RoleViewer, func(user auth.User) {
			s.admin.Logout(cookieValue(r, sessionCookie))
			s.record(r.Context(), user, "admin.logout", "session", "")
			clearCookie(w, sessionCookie)
			clearCookie(w, csrfCookie)
			w.WriteHeader(http.StatusNoContent)
		})
	case "/api/v1/auth/me":
		s.require(w, r, auth.RoleViewer, func(user auth.User) { writeJSON(w, http.StatusOK, user) })
	case "/api/v1/traffic/live":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.liveTraffic(w) })
	case "/api/v1/traffic/history":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficHistory(w, r) })
	case "/api/v1/traffic/summary":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficSummary(w, r) })
	case "/api/v1/traffic/timeseries":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficTimeseries(w, r) })
	case "/api/v1/audit":
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.listAudit(w, r) })
	case "/api/v1/proxies":
		switch r.Method {
		case http.MethodGet:
			s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listProxies(w, r) })
		case http.MethodPost:
			s.createProxy(w, r)
		default:
			methodNotAllowed(w)
		}
	case "/api/v1/proxies/import":
		s.importProxies(w, r)
	case "/api/v1/proxies/import/preview":
		s.previewImport(w, r)
	case "/api/v1/clients":
		switch r.Method {
		case http.MethodGet:
			s.listClients(w, r)
		case http.MethodPost:
			s.createClient(w, r)
		default:
			methodNotAllowed(w)
		}
	default:
		if strings.HasPrefix(r.URL.Path, "/api/v1/clients/") && strings.HasSuffix(r.URL.Path, "/api-keys") {
			switch r.Method {
			case http.MethodGet:
				s.listAPIKeys(w, r)
			case http.MethodPost:
				s.createAPIKey(w, r)
			default:
				methodNotAllowed(w)
			}
		} else if strings.Contains(r.URL.Path, "/api-keys/") {
			if r.Method != http.MethodDelete {
				methodNotAllowed(w)
				return
			}
			s.revokeAPIKey(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/proxies/") {
			s.proxyByID(w, r)
		} else {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "The requested API resource was not found.")
		}
	}
}
func (s *Server) listClients(w http.ResponseWriter, r *http.Request) {
	s.require(w, r, auth.RoleAdmin, func(_ auth.User) {
		reader, ok := s.clients.(ClientReader)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 1000 {
				writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
				return
			}
			limit = value
		}
		clients, err := reader.ListClients(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": clients})
	})
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	s.require(w, r, auth.RoleAdmin, func(_ auth.User) {
		reader, ok := s.clients.(ClientReader)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		clientID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/clients/"), "/api-keys")
		if !model.ID(clientID).Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_CLIENT", "Client ID was not accepted.")
			return
		}
		keys, err := reader.ListAPIKeys(r.Context(), model.ID(clientID), 100)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": keys})
	})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) {
		revoker, ok := s.clients.(APIKeyRevoker)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/clients/")
		parts := strings.Split(path, "/api-keys/")
		if len(parts) != 2 || !model.ID(parts[0]).Valid() || !model.ID(parts[1]).Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_KEY", "API key identity was not accepted.")
			return
		}
		if err := revoker.RevokeAPIKey(r.Context(), model.ID(parts[0]), model.ID(parts[1]), s.now()); err != nil {
			writeError(w, http.StatusNotFound, "KEY_NOT_FOUND", "The API key was not found or was already revoked.")
			return
		}
		s.record(r.Context(), user, "api_key.revoked", "client", parts[0])
		w.WriteHeader(http.StatusNoContent)
	})
}
func (s *Server) createClient(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) {
		if s.clients == nil {
			writeError(w, 503, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		var input struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &input) {
			return
		}
		client := auth.Client{ID: model.NewID(), Name: input.Name, Enabled: true, AuthMethod: "api_key", CreatedAt: s.now()}
		if err := s.clients.CreateClient(r.Context(), client); err != nil {
			writeError(w, 400, "INVALID_CLIENT", "Client metadata was not accepted.")
			return
		}
		s.record(r.Context(), user, "client.created", "client", string(client.ID))
		writeJSON(w, 201, client)
	})
}
func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) {
		if s.clients == nil {
			writeError(w, 503, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		clientID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/clients/"), "/api-keys")
		if !model.ID(clientID).Valid() {
			writeError(w, 400, "INVALID_CLIENT", "Client ID was not accepted.")
			return
		}
		token, prefix, hash, err := keys.Generate()
		if err != nil {
			writeError(w, 503, "KEY_UNAVAILABLE", "A key could not be generated.")
			return
		}
		key := auth.APIKey{ID: model.NewID(), ClientID: model.ID(clientID), Prefix: prefix, CreatedAt: s.now()}
		created, err := s.clients.CreateAPIKey(r.Context(), key, hash)
		if err != nil {
			writeError(w, 400, "KEY_NOT_CREATED", "API key could not be stored.")
			return
		}
		s.record(r.Context(), user, "api_key.created", "client", clientID)
		writeJSON(w, 201, map[string]any{"api_key": created, "token": token})
	})
}
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	required, err := s.admin.SetupRequired(r.Context())
	if err != nil {
		writeError(w, 500, "STORE_UNAVAILABLE", "Control-plane storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": required})
}
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	user, err := s.admin.Setup(r.Context(), input.Token, input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusForbidden, "SETUP_REJECTED", "Setup token or account details were not accepted.")
		return
	}
	token, err := s.admin.CreateSession(user)
	if err != nil {
		writeError(w, 503, "SESSION_UNAVAILABLE", "Account created. Sign in again to create a session.")
		return
	}
	s.createSessionToken(w, token)
	s.record(r.Context(), user, "admin.setup", "user", string(user.ID))
	writeJSON(w, http.StatusCreated, user)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	token, user, err := s.admin.Login(r.Context(), input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Username or password was not accepted.")
		return
	}
	s.createSessionToken(w, token)
	s.record(r.Context(), user, "admin.login", "user", string(user.ID))
	writeJSON(w, http.StatusOK, user)
}
func (s *Server) createSessionToken(w http.ResponseWriter, token string) {
	csrf := s.admin.CSRFToken(token)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60, Secure: false})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: csrf, Path: "/", HttpOnly: false, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60, Secure: false})
}
func (s *Server) require(w http.ResponseWriter, r *http.Request, role auth.Role, next func(auth.User)) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	user, err := s.admin.Require(cookieValue(r, sessionCookie), role)
	if errors.Is(err, admin.ErrForbidden) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Your role cannot access this resource.")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in is required.")
		return
	}
	next(user)
}
func (s *Server) requireMutation(w http.ResponseWriter, r *http.Request, role auth.Role, next func(auth.User)) {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	if !s.admin.ValidCSRF(cookieValue(r, sessionCookie), r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "CSRF_REJECTED", "CSRF token validation failed.")
		return
	}
	user, err := s.admin.Require(cookieValue(r, sessionCookie), role)
	if errors.Is(err, admin.ErrForbidden) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Your role cannot modify this resource.")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Sign in is required.")
		return
	}
	next(user)
}
func (s *Server) liveTraffic(w http.ResponseWriter) {
	durable := internaltraffic.AsyncStats{}
	if s.trafficStatus != nil {
		durable = s.trafficStatus.Stats()
	}
	if s.traffic == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}, "dropped": 0, "durable": durable})
		return
	}
	events, dropped := s.traffic.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "dropped": dropped, "durable": durable})
}
func (s *Server) trafficHistory(w http.ResponseWriter, r *http.Request) {
	if s.trafficStore == nil {
		writeJSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			writeError(w, 400, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
		limit = value
	}
	events, err := s.trafficStore.ListTraffic(r.Context(), sqlite.TrafficPage{Limit: limit})
	if err != nil {
		writeError(w, 503, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	writeJSON(w, 200, map[string]any{"items": events})
}

func (s *Server) trafficSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query, ok := s.parseTrafficQuery(r, false)
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_RANGE", "The traffic query range or filters were not accepted.")
		return
	}
	if s.trafficStore == nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	summary, err := s.trafficStore.TrafficSummary(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) trafficTimeseries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query, ok := s.parseTrafficQuery(r, true)
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_RANGE", "The traffic query range, granularity or filters were not accepted.")
		return
	}
	if s.trafficStore == nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	series, err := s.trafficStore.TrafficTimeseries(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, series)
}

func (s *Server) parseTrafficQuery(r *http.Request, series bool) (sqlite.TrafficQuery, bool) {
	values := r.URL.Query()
	granularity := time.Duration(0)
	if series {
		switch values.Get("granularity") {
		case "", "hour":
			granularity = time.Hour
		case "minute":
			granularity = time.Minute
		case "day":
			granularity = 24 * time.Hour
		default:
			return sqlite.TrafficQuery{}, false
		}
	}
	width := granularity
	if width == 0 {
		width = time.Minute
	}
	until := s.now().UTC().Truncate(width).Add(width)
	from := until.Add(-24 * time.Hour)
	rawFrom, rawUntil := values.Get("from"), values.Get("until")
	if rawFrom != "" || rawUntil != "" {
		if rawFrom == "" || rawUntil == "" {
			return sqlite.TrafficQuery{}, false
		}
		var err error
		from, err = time.Parse(time.RFC3339Nano, rawFrom)
		if err != nil {
			return sqlite.TrafficQuery{}, false
		}
		until, err = time.Parse(time.RFC3339Nano, rawUntil)
		if err != nil {
			return sqlite.TrafficQuery{}, false
		}
	}
	query := sqlite.TrafficQuery{
		From: from.UTC(), Until: until.UTC(), Granularity: granularity,
		ClientID: model.ID(values.Get("client_id")), PoolID: model.ID(values.Get("pool_id")), ProxyID: model.ID(values.Get("proxy_id")),
		Action: values.Get("action"), Protocol: values.Get("protocol"),
	}
	if series {
		return query, query.ValidSeries()
	}
	return query, query.ValidSummary()
}
func (s *Server) listProxies(w http.ResponseWriter, r *http.Request) {
	if s.endpoints == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "next_after": ""})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
		limit = value
	}
	records, err := s.endpoints.List(r.Context(), store.Page{After: model.ID(r.URL.Query().Get("after")), Limit: limit})
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
		return
	}
	next := ""
	if len(records) == limit {
		next = string(records[len(records)-1].Endpoint.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records, "next_after": next})
}
func (s *Server) createProxy(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
		if s.endpoints == nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy storage is unavailable.")
			return
		}
		var input struct {
			Endpoint proxy.Endpoint `json:"endpoint"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Endpoint.ID == "" {
			input.Endpoint.ID = model.NewID()
		}
		if input.Endpoint.Validate() != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PROXY", "Proxy metadata was not accepted.")
			return
		}
		record, err := s.endpoints.Put(r.Context(), input.Endpoint, 0)
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "PROXY_EXISTS", "A proxy with this ID already exists.")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy metadata could not be stored.")
			return
		}
		s.record(r.Context(), user, "proxy.created", "proxy", string(record.Endpoint.ID))
		writeJSON(w, http.StatusCreated, map[string]any{"proxy": record, "runtime_active": false, "activation": "inventory_only"})
	})
}

func (s *Server) proxyByID(w http.ResponseWriter, r *http.Request) {
	id := model.ID(strings.TrimPrefix(r.URL.Path, "/api/v1/proxies/"))
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_PROXY", "Proxy ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getProxy(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updateProxy(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deleteProxy(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) getProxy(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.endpoints == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy storage is unavailable.")
		return
	}
	record, err := s.endpoints.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "PROXY_NOT_FOUND", "The proxy was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy metadata could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proxy": record})
}

func (s *Server) updateProxy(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.endpoints == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy storage is unavailable.")
		return
	}
	var input struct {
		Endpoint proxy.Endpoint `json:"endpoint"`
		Revision int64          `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Endpoint.ID != id || input.Revision < 1 || input.Endpoint.Validate() != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROXY", "Proxy metadata or revision was not accepted.")
		return
	}
	record, err := s.endpoints.Put(r.Context(), input.Endpoint, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "proxy.updated", "proxy", string(id))
		writeJSON(w, http.StatusOK, map[string]any{"proxy": record, "runtime_active": false, "activation": "inventory_only"})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "PROXY_NOT_FOUND", "The proxy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "PROXY_CONFLICT", "The proxy revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy metadata could not be stored.")
	}
}

func (s *Server) deleteProxy(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.endpoints == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Proxy revision was not accepted.")
		return
	}
	err := s.endpoints.Delete(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "proxy.deleted", "proxy", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "PROXY_NOT_FOUND", "The proxy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "PROXY_CONFLICT", "The proxy revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy metadata could not be deleted.")
	}
}

func (s *Server) importProxies(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
		endpointStore, ok := s.endpoints.(store.EndpointStore)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy storage does not support atomic imports.")
			return
		}
		var input struct {
			Input string              `json:"input"`
			Mode  proxy.DuplicateMode `json:"mode"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Mode == "" {
			input.Mode = proxy.SkipDuplicates
		}
		if input.Input == "" {
			writeError(w, http.StatusBadRequest, "INVALID_IMPORT", "Proxy import text was not accepted.")
			return
		}
		var result proxy.ImportResult
		var records []store.EndpointRecord
		createdCount := 0
		err := endpointStore.WithinTransaction(r.Context(), func(tx store.Endpoints) error {
			existing, err := allEndpointRecords(r.Context(), tx)
			if err != nil {
				return err
			}
			result, err = proxy.Import(proxy.ImportRequest{Input: input.Input, Mode: input.Mode, Existing: existingByIdentity(existing)})
			if err != nil {
				return err
			}
			records = make([]store.EndpointRecord, 0, len(result.Endpoints))
			for _, endpoint := range result.Endpoints {
				expected := int64(0)
				if input.Mode == proxy.UpdateDuplicates {
					expected = existing[proxy.Identity(endpoint)].Revision
				}
				record, err := tx.Put(r.Context(), endpoint, expected)
				if err != nil {
					return err
				}
				if expected == 0 {
					createdCount++
				}
				records = append(records, record)
			}
			return nil
		})
		if err != nil {
			switch {
			case errors.Is(err, proxy.ErrParse), errors.Is(err, store.ErrInvalid):
				writeError(w, http.StatusBadRequest, "INVALID_IMPORT", "Proxy import text or mode was not accepted.")
			case errors.Is(err, store.ErrConflict):
				writeError(w, http.StatusConflict, "IMPORT_CONFLICT", "Proxy inventory changed during import.")
			case errors.Is(err, store.ErrNotFound):
				writeError(w, http.StatusNotFound, "PROXY_NOT_FOUND", "A proxy referenced by the import was not found.")
			default:
				writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy import could not be committed.")
			}
			return
		}
		for _, record := range records {
			s.record(r.Context(), user, "proxy.imported", "proxy", string(record.Endpoint.ID))
		}
		writeJSON(w, http.StatusCreated, map[string]any{"items": records, "created": createdCount, "updated": result.Updated, "skipped": result.Skipped, "mode": input.Mode})
	})
}

func allEndpointRecords(ctx context.Context, endpoints store.Endpoints) (map[string]store.EndpointRecord, error) {
	const pageSize = 1000
	rows := make(map[string]store.EndpointRecord)
	after := model.ID("")
	for {
		page, err := endpoints.List(ctx, store.Page{After: after, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		for _, record := range page {
			rows[proxy.Identity(record.Endpoint)] = record
		}
		if len(page) < pageSize {
			return rows, nil
		}
		after = page[len(page)-1].Endpoint.ID
	}
}

func existingByIdentity(records map[string]store.EndpointRecord) map[string]proxy.Endpoint {
	existing := make(map[string]proxy.Endpoint, len(records))
	for _, record := range records {
		existing[proxy.Identity(record.Endpoint)] = record.Endpoint
	}
	return existing
}

func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleOperator, func(_ auth.User) {
		var input struct {
			Input string `json:"input"`
		}
		if !decode(w, r, &input) {
			return
		}
		preview, err := proxy.Preview(input.Input, 100_000)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_IMPORT", "Proxy import text was not accepted.")
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
}
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	reader, ok := s.audit.(audit.Reader)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			writeError(w, 400, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
		limit = value
	}
	events, err := reader.ListAudit(r.Context(), audit.Page{Limit: limit})
	if err != nil {
		writeError(w, 503, "AUDIT_UNAVAILABLE", "Audit storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}
func (s *Server) record(ctx context.Context, user auth.User, action, targetType, targetID string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Record(context.WithoutCancel(ctx), audit.Event{ID: model.NewID(), At: s.now(), ActorID: user.ID, Action: action, TargetType: targetType, TargetID: targetID})
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "JSON_REQUIRED", "Use application/json for this request.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request body was not accepted.")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "This method is not supported for the resource.")
}
func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}
func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == sessionCookie, SameSite: http.SameSiteStrictMode})
}
func safeHost(value string) bool {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // Non-browser clients must still present JSON and session-bound CSRF.
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func (s *Server) allowAuthAttempt() bool {
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	now := s.now()
	if s.windowStart.IsZero() || now.Sub(s.windowStart) >= time.Minute {
		s.windowStart = now
		s.authAttempts = 0
	}
	if s.authAttempts >= 30 {
		return false
	}
	s.authAttempts++
	return true
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
