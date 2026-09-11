// Package api provides the versioned local control-plane HTTP boundary.
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const sessionCookie = "proxysieve_session"
const csrfCookie = "proxysieve_csrf"
const maxBodyBytes = 64 << 10

type Server struct {
	admin     *admin.Service
	traffic   *internaltraffic.Memory
	endpoints store.Endpoints
	now       func() time.Time
}

func New(service *admin.Service, traffic *internaltraffic.Memory, endpoints store.Endpoints) (*Server, error) {
	if service == nil {
		return nil, errors.New("admin service is required")
	}
	return &Server{admin: service, traffic: traffic, endpoints: endpoints, now: func() time.Time { return time.Now().UTC() }}, nil
}
func (s *Server) Handler() http.Handler { return securityHeaders(http.HandlerFunc(s.handle)) }
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if !safeHost(r.Host) {
		writeError(w, http.StatusBadRequest, "INVALID_HOST", "The request host is not allowed.")
		return
	}
	switch r.URL.Path {
	case "/health":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/ready":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
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
		s.requireMutation(w, r, auth.RoleViewer, func(_ auth.User) {
			s.admin.Logout(cookieValue(r, sessionCookie))
			clearCookie(w, sessionCookie)
			clearCookie(w, csrfCookie)
			w.WriteHeader(http.StatusNoContent)
		})
	case "/api/v1/auth/me":
		s.require(w, r, auth.RoleViewer, func(user auth.User) { writeJSON(w, http.StatusOK, user) })
	case "/api/v1/traffic/live":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.liveTraffic(w) })
	case "/api/v1/proxies":
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listProxies(w, r) })
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "The requested API resource was not found.")
	}
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
	s.createSession(w, user)
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
	writeJSON(w, http.StatusOK, user)
}
func (s *Server) createSession(w http.ResponseWriter, user auth.User) {
	token, err := s.admin.CreateSession(user)
	if err == nil {
		s.createSessionToken(w, token)
	}
}
func (s *Server) createSessionToken(w http.ResponseWriter, token string) {
	csrf := randomToken()
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
	if subtle.ConstantTimeCompare([]byte(cookieValue(r, csrfCookie)), []byte(r.Header.Get("X-CSRF-Token"))) != 1 || cookieValue(r, csrfCookie) == "" {
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
	if s.traffic == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}, "dropped": 0})
		return
	}
	events, dropped := s.traffic.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "dropped": dropped})
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
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
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
func randomToken() string {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func safeHost(value string) bool {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
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
