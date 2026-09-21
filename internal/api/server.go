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
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	internalalert "github.com/nguyenduytan/proxysieve/internal/alert"
	"github.com/nguyenduytan/proxysieve/internal/audit"
	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
	internalcache "github.com/nguyenduytan/proxysieve/internal/cache"
	"github.com/nguyenduytan/proxysieve/internal/downstreamauth"
	internalevents "github.com/nguyenduytan/proxysieve/internal/events"
	"github.com/nguyenduytan/proxysieve/internal/keys"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internalsource "github.com/nguyenduytan/proxysieve/internal/source"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	publicalert "github.com/nguyenduytan/proxysieve/pkg/alert"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	publicevent "github.com/nguyenduytan/proxysieve/pkg/event"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	publictraffic "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

const sessionCookie = "proxysieve_session"
const csrfCookie = "proxysieve_csrf"
const maxBodyBytes = 64 << 10
const maxImportBodyBytes = 9 << 20

//go:embed openapi.yaml
var openAPISpec []byte

type Server struct {
	admin           *admin.Service
	traffic         *internaltraffic.Memory
	endpoints       store.Endpoints
	sources         store.Sources
	pools           store.Pools
	chains          store.Chains
	policies        store.Policies
	clients         ClientStore
	trafficStore    TrafficStore
	trafficStatus   TrafficStatus
	runtimeControl  RuntimeControl
	browserAuth     downstreamauth.Store
	browserRecorder publictraffic.Recorder
	sessions        SessionStore
	health          HealthControl
	budgets         *internalbudget.Manager
	responseCache   internalcache.ResponseStore
	alerts          store.Alerts
	alertControl    AlertControl
	shadows         store.Shadows
	shadowControl   ShadowControl
	audit           audit.Writer
	events          *internalevents.Bus
	now             func() time.Time
	sourceResolver  internalsource.Resolver
	sourcePolicy    security.DestinationPolicy
	sourceRefresh   *internalsource.Refresher
	ui              http.Handler
	limitMu         sync.Mutex
	routingMu       sync.Mutex
	chainHealthMu   sync.RWMutex
	chainHealth     map[model.ID]chainHealthRecord
	chainTester     ChainTester
	listenerMetrics []ListenerMetric
	windowStart     time.Time
	authAttempts    int
}

type ClientStore interface {
	GetClient(context.Context, model.ID) (store.ClientRecord, error)
	PutClient(context.Context, auth.Client, int64) (store.ClientRecord, error)
	DeleteClient(context.Context, model.ID, int64) error
	ListClients(context.Context, store.Page) ([]store.ClientRecord, error)
	CreateAPIKey(context.Context, auth.APIKey, [32]byte) (auth.APIKey, error)
	ListAPIKeys(context.Context, model.ID, int) ([]auth.APIKey, error)
	RevokeAPIKey(context.Context, model.ID, model.ID, time.Time) error
}

type clientRepresentation struct {
	auth.Client
	Revision int64 `json:"revision"`
}
type TrafficStore interface {
	ListTraffic(context.Context, sqlite.TrafficPage) ([]publictraffic.Event, error)
	TrafficSummary(context.Context, sqlite.TrafficQuery) (publictraffic.Summary, error)
	TrafficTimeseries(context.Context, sqlite.TrafficQuery) (publictraffic.Series, error)
	TrafficBreakdown(context.Context, sqlite.TrafficQuery, string, int) (publictraffic.Breakdown, error)
}
type TrafficStatus interface {
	Stats() internaltraffic.AsyncStats
}
type SessionStore interface {
	List(context.Context) ([]publicsession.Session, error)
	Get(context.Context, model.ID) (publicsession.Session, error)
	Rotate(context.Context, model.ID, publicsession.RotationReason) error
	Delete(context.Context, model.ID) error
}

type AlertControl interface {
	Stats() internalalert.Stats
	Test(context.Context, model.ID) (publicalert.Delivery, error)
}

type ShadowControl interface {
	Upsert(publicshadow.Config)
	Delete(model.ID)
	Comparison(model.ID) (publicshadow.Comparison, bool)
}

type ListenerMetric struct {
	Name           string
	Type           string
	Bind           string
	MaxConnections int
	Metrics        *security.ListenerMetrics
}

type listenerStatus struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Bind           string `json:"bind"`
	MaxConnections int    `json:"max_connections"`
	security.ListenerStats
}

func New(service *admin.Service, traffic *internaltraffic.Memory, endpoints store.Endpoints, auditWriter audit.Writer, clientStores ...ClientStore) (*Server, error) {
	if service == nil {
		return nil, errors.New("admin service is required")
	}
	var clients ClientStore
	if len(clientStores) > 0 {
		clients = clientStores[0]
	}
	var browserAuth downstreamauth.Store
	if authStore, ok := clients.(downstreamauth.Store); ok {
		browserAuth = authStore
	}
	var durable TrafficStore
	if source, ok := endpoints.(TrafficStore); ok {
		durable = source
	}
	var sources store.Sources
	if sourceStore, ok := endpoints.(store.Sources); ok {
		sources = sourceStore
	}
	var pools store.Pools
	if poolStore, ok := endpoints.(store.Pools); ok {
		pools = poolStore
	}
	var policies store.Policies
	if policyStore, ok := endpoints.(store.Policies); ok {
		policies = policyStore
	}
	var chains store.Chains
	if chainStore, ok := endpoints.(store.Chains); ok {
		chains = chainStore
	}
	var alerts store.Alerts
	if alertStore, ok := endpoints.(store.Alerts); ok {
		alerts = alertStore
	}
	var shadows store.Shadows
	if shadowStore, ok := endpoints.(store.Shadows); ok {
		shadows = shadowStore
	}
	return &Server{
		admin: service, traffic: traffic, endpoints: endpoints, sources: sources, pools: pools, chains: chains, policies: policies,
		clients: clients, trafficStore: durable, browserAuth: browserAuth, browserRecorder: traffic, alerts: alerts, shadows: shadows, audit: auditWriter, ui: dashboardHandler(),
		chainHealth:    map[model.ID]chainHealthRecord{},
		now:            func() time.Time { return time.Now().UTC() },
		sourceResolver: sourceResolver{},
		sourcePolicy:   security.DestinationPolicy{DenyPrivate: true},
		sourceRefresh:  internalsource.NewRefresher(""),
	}, nil
}

type sourceResolver struct{}

func (sourceResolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
func (s *Server) Handler() http.Handler                              { return securityHeaders(http.HandlerFunc(s.handle)) }
func (s *Server) SetTrafficStatus(status TrafficStatus)              { s.trafficStatus = status }
func (s *Server) SetEvents(events *internalevents.Bus)               { s.events = events }
func (s *Server) SetAlertControl(control AlertControl)               { s.alertControl = control }
func (s *Server) SetShadowControl(control ShadowControl)             { s.shadowControl = control }
func (s *Server) SetRuntimeControl(control RuntimeControl)           { s.runtimeControl = control }
func (s *Server) SetBrowserRecorder(recorder publictraffic.Recorder) { s.browserRecorder = recorder }
func (s *Server) SetSessions(sessions SessionStore)                  { s.sessions = sessions }
func (s *Server) SetHealth(health HealthControl)                     { s.health = health }
func (s *Server) SetBudgets(budgets *internalbudget.Manager)         { s.budgets = budgets }
func (s *Server) SetCache(responseCache internalcache.ResponseStore) { s.responseCache = responseCache }
func (s *Server) SetChainTester(tester ChainTester)                  { s.chainTester = tester }
func (s *Server) SetListenerMetrics(metrics []ListenerMetric) {
	s.listenerMetrics = append([]ListenerMetric(nil), metrics...)
}
func (s *Server) SetSourceRefresher(refresher *internalsource.Refresher) {
	if refresher != nil {
		s.sourceRefresh = refresher
	}
}
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
			listeners := make([]listenerStatus, 0, len(s.listenerMetrics))
			for _, metric := range s.listenerMetrics {
				listeners = append(listeners, listenerStatus{Name: metric.Name, Type: metric.Type, Bind: metric.Bind, MaxConnections: metric.MaxConnections, ListenerStats: metric.Metrics.Snapshot()})
			}
			writeJSON(w, http.StatusOK, map[string]any{"build": buildinfo.Current(), "user": user, "listeners": listeners})
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
	case "/api/v1/users":
		s.usersCollection(w, r)
	case "/api/v1/traffic/live":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.liveTraffic(w) })
	case "/api/v1/traffic/stream":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.streamTraffic(w, r) })
	case "/api/v1/events":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listEvents(w, r) })
	case "/api/v1/events/stream":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.streamEvents(w, r) })
	case "/api/v1/alerts":
		s.alertsCollection(w, r)
	case "/api/v1/webhooks":
		s.webhooksCollection(w, r)
	case "/api/v1/traffic/history":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficHistory(w, r) })
	case "/api/v1/traffic/summary":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficSummary(w, r) })
	case "/api/v1/traffic/timeseries":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficTimeseries(w, r) })
	case "/api/v1/traffic/breakdown":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.trafficBreakdown(w, r) })
	case "/api/v1/browser/policy":
		s.browserPolicy(w, r)
	case "/api/v1/browser/blocks":
		s.browserBlocks(w, r)
	case "/api/v1/budgets":
		s.budgetsCollection(w, r)
	case "/api/v1/cache/stats":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.cacheStats(w) })
	case "/api/v1/cache/purge":
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.purgeCache(w, r, user) })
	case "/api/v1/cache/purge/domain":
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.purgeCacheDomain(w, r, user) })
	case "/api/v1/health/proxies":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.proxyHealth(w, r) })
	case "/api/v1/health/pools":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.poolHealth(w, r) })
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
	case "/api/v1/sources":
		s.sourcesCollection(w, r)
	case "/api/v1/pools":
		s.poolsCollection(w, r)
	case "/api/v1/chains":
		s.chainsCollection(w, r)
	case "/api/v1/policies":
		s.policiesCollection(w, r)
	case "/api/v1/shadow":
		s.shadowCollection(w, r)
	case "/api/v1/sessions":
		s.sessionsCollection(w, r)
	case "/api/v1/runtime":
		s.runtimeStatus(w, r)
	case "/api/v1/runtime/history":
		s.runtimeHistory(w, r)
	case "/api/v1/runtime/activate":
		s.runtimeActivate(w, r)
	case "/api/v1/runtime/rollback":
		s.runtimeRollback(w, r)
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
		if strings.HasPrefix(r.URL.Path, "/api/v1/health/proxies/") && strings.HasSuffix(r.URL.Path, "/check") {
			s.proxyHealthCheck(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/health/pools/") && strings.HasSuffix(r.URL.Path, "/check") {
			s.poolHealthCheck(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/clients/") && strings.HasSuffix(r.URL.Path, "/api-keys") {
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
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/budgets/") {
			s.budgetByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/alerts/") {
			s.alertByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/webhooks/") {
			s.webhookByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/clients/") {
			s.clientByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
			s.userByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/proxies/") {
			s.proxyByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/sources/") {
			s.sourceByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/shadow/") {
			s.shadowByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/policies/") && strings.HasSuffix(r.URL.Path, "/simulate") {
			s.simulatePolicy(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/sessions/") {
			s.sessionByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/pools/") {
			s.poolByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/chains/") && strings.HasSuffix(r.URL.Path, "/test") {
			s.chainTest(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/chains/") {
			s.chainByID(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/policies/") {
			s.policyByID(w, r)
		} else {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "The requested API resource was not found.")
		}
	}
}
func (s *Server) listClients(w http.ResponseWriter, r *http.Request) {
	s.require(w, r, auth.RoleAdmin, func(_ auth.User) {
		if s.clients == nil {
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
		page := store.Page{After: model.ID(r.URL.Query().Get("after")), Limit: limit}
		if err := page.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
		records, err := s.clients.ListClients(r.Context(), page)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		items := make([]clientRepresentation, 0, len(records))
		for _, record := range records {
			items = append(items, representClient(record))
		}
		next := ""
		if len(records) == limit {
			next = string(records[len(records)-1].Client.ID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_after": next})
	})
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	s.require(w, r, auth.RoleAdmin, func(_ auth.User) {
		if s.clients == nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		clientID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/clients/"), "/api-keys")
		if !model.ID(clientID).Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_CLIENT", "Client ID was not accepted.")
			return
		}
		keys, err := s.clients.ListAPIKeys(r.Context(), model.ID(clientID), 100)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": keys})
	})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) {
		if s.clients == nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/clients/")
		parts := strings.Split(path, "/api-keys/")
		if len(parts) != 2 || !model.ID(parts[0]).Valid() || !model.ID(parts[1]).Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_KEY", "API key identity was not accepted.")
			return
		}
		if err := s.clients.RevokeAPIKey(r.Context(), model.ID(parts[0]), model.ID(parts[1]), s.now()); err != nil {
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
		record, err := s.clients.PutClient(r.Context(), client, 0)
		switch {
		case err == nil:
			s.record(r.Context(), user, "client.created", "client", string(client.ID))
			writeJSON(w, http.StatusCreated, representClient(record))
		case errors.Is(err, store.ErrConflict):
			writeError(w, http.StatusConflict, "CLIENT_EXISTS", "A client with this ID already exists.")
		default:
			writeError(w, http.StatusBadRequest, "INVALID_CLIENT", "Client metadata was not accepted.")
		}
	})
}

func (s *Server) clientByID(w http.ResponseWriter, r *http.Request) {
	id := model.ID(strings.TrimPrefix(r.URL.Path, "/api/v1/clients/"))
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_CLIENT", "Client ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.getClient(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.updateClient(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.deleteClient(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) getClient(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.clients == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
		return
	}
	record, err := s.clients.GetClient(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, representClient(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "CLIENT_NOT_FOUND", "The client was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
	}
}

func (s *Server) updateClient(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.clients == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
		return
	}
	var input struct {
		Client   auth.Client `json:"client"`
		Revision int64       `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Client.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_CLIENT", "Client metadata or revision was not accepted.")
		return
	}
	current, err := s.clients.GetClient(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "CLIENT_NOT_FOUND", "The client was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
		return
	}
	input.Client.CreatedAt = current.Client.CreatedAt
	input.Client.LastSeenAt = current.Client.LastSeenAt
	input.Client.AuthMethod = current.Client.AuthMethod
	record, err := s.clients.PutClient(r.Context(), input.Client, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "client.updated", "client", string(id))
		writeJSON(w, http.StatusOK, representClient(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "CLIENT_NOT_FOUND", "The client was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "CLIENT_CONFLICT", "The client revision is stale.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_CLIENT", "Client metadata or revision was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client metadata could not be stored.")
	}
}

func (s *Server) deleteClient(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.clients == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Client revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if s.budgets != nil && s.budgets.References(publicbudget.ScopeClient, id) {
		writeError(w, http.StatusConflict, "CLIENT_IN_USE", "The client is referenced by a saved budget.")
		return
	}
	err := s.clients.DeleteClient(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "client.deleted", "client", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "CLIENT_NOT_FOUND", "The client was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "CLIENT_CONFLICT", "The client revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Client metadata could not be deleted.")
	}
}

func representClient(record store.ClientRecord) clientRepresentation {
	return clientRepresentation{Client: record.Client.Clone(), Revision: record.Revision}
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

func (s *Server) trafficBreakdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query, ok := s.parseTrafficQuery(r, false)
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_RANGE", "The traffic query range or filters were not accepted.")
		return
	}
	dimension := r.URL.Query().Get("dimension")
	switch dimension {
	case "client", "pool", "proxy", "chain", "policy", "rule", "action", "protocol":
	default:
		writeError(w, http.StatusBadRequest, "INVALID_DIMENSION", "The traffic breakdown dimension was not accepted.")
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "The traffic breakdown limit was not accepted.")
			return
		}
		limit = value
	}
	if s.trafficStore == nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	breakdown, err := s.trafficStore.TrafficBreakdown(r.Context(), query, dimension, limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "TRAFFIC_UNAVAILABLE", "Traffic storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, breakdown)
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
		ClientID: model.ID(values.Get("client_id")), PoolID: model.ID(values.Get("pool_id")), ProxyID: model.ID(values.Get("proxy_id")), ChainID: model.ID(values.Get("chain_id")),
		PolicyID: model.ID(values.Get("policy_id")), RuleID: model.ID(values.Get("rule_id")), Action: values.Get("action"), Protocol: values.Get("protocol"),
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
		writeJSON(w, http.StatusCreated, s.proxyResponse(record))
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
	writeJSON(w, http.StatusOK, s.proxyResponse(record))
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
		writeJSON(w, http.StatusOK, s.proxyResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "PROXY_NOT_FOUND", "The proxy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "PROXY_CONFLICT", "The proxy revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Proxy metadata could not be stored.")
	}
}

func (s *Server) proxyResponse(record store.EndpointRecord) map[string]any {
	active, revision := false, int64(0)
	if s.runtimeControl != nil {
		current := s.runtimeControl.CurrentRuntime()
		revision = current.Revision
		for _, endpoint := range current.Bundle.Proxies {
			if endpoint.ID == record.Endpoint.ID && reflect.DeepEqual(endpoint, record.Endpoint) {
				active = true
				break
			}
		}
	}
	activation := "staged"
	if active {
		activation = "active"
	}
	return map[string]any{"proxy": record, "runtime_active": active, "activation": activation, "runtime_revision": revision}
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
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if s.pools != nil {
		referenced, err := s.poolUsesEndpoint(r, id)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool references could not be checked.")
			return
		}
		if referenced {
			writeError(w, http.StatusConflict, "PROXY_IN_USE", "The proxy is assigned to a pool.")
			return
		}
	}
	if s.budgets != nil && s.budgets.References(publicbudget.ScopeProxy, id) {
		writeError(w, http.StatusConflict, "PROXY_IN_USE", "The proxy is referenced by a saved budget.")
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
			Input   string              `json:"input"`
			Mode    proxy.DuplicateMode `json:"mode"`
			Format  proxy.ImportFormat  `json:"format"`
			Mapping proxy.ImportMapping `json:"mapping"`
		}
		if !decodeBounded(w, r, &input, maxImportBodyBytes) {
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
			result, err = proxy.Import(proxy.ImportRequest{
				Input: input.Input, Mode: input.Mode, Existing: existingByIdentity(existing),
				Options: proxy.ImportOptions{Format: input.Format, Mapping: input.Mapping},
			})
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
			Input   string              `json:"input"`
			Format  proxy.ImportFormat  `json:"format"`
			Mapping proxy.ImportMapping `json:"mapping"`
		}
		if !decodeBounded(w, r, &input, maxImportBodyBytes) {
			return
		}
		preview, err := proxy.PreviewWithOptions(input.Input, 100_000, proxy.ImportOptions{Format: input.Format, Mapping: input.Mapping})
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
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "next_before": "", "next_before_id": ""})
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
	page := audit.Page{Limit: limit}
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
		page.Before = before
		page.BeforeID = model.ID(r.URL.Query().Get("before_id"))
		if !page.Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
			return
		}
	} else if r.URL.Query().Get("before_id") != "" {
		writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
		return
	}
	events, err := reader.ListAudit(r.Context(), page)
	if err != nil {
		writeError(w, 503, "AUDIT_UNAVAILABLE", "Audit storage is unavailable.")
		return
	}
	nextBefore, nextBeforeID := "", ""
	if len(events) == limit {
		last := events[len(events)-1]
		nextBefore, nextBeforeID = last.At.Format(time.RFC3339Nano), string(last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events, "next_before": nextBefore, "next_before_id": nextBeforeID})
}
func (s *Server) record(ctx context.Context, user auth.User, action, targetType, targetID string) {
	id, at := model.NewID(), s.now()
	ctx = context.WithoutCancel(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.Event{ID: id, At: at, ActorID: user.ID, Action: action, TargetType: targetType, TargetID: targetID})
	}
	if s.events != nil {
		severity := publicevent.Info
		if action == "source.refresh_failed" {
			severity = publicevent.Error
		}
		_ = s.events.Publish(ctx, publicevent.Event{ID: id, At: at, Type: action, Severity: severity, Source: "admin", ActorID: user.ID, TargetType: targetType, TargetID: targetID})
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeBounded(w, r, target, maxBodyBytes)
}

func decodeBounded(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "JSON_REQUIRED", "Use application/json for this request.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
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
