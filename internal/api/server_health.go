package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publichealth "github.com/nguyenduytan/proxysieve/pkg/health"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

var ErrHealthBusy = errors.New("health check already in progress")
var ErrHealthTargetDenied = errors.New("health check target denied")

type ProxyHealth struct {
	ProxyID             model.ID             `json:"proxy_id"`
	Name                string               `json:"name"`
	State               publichealth.State   `json:"state"`
	Circuit             publichealth.Circuit `json:"circuit"`
	Score               uint8                `json:"score"`
	Latency             time.Duration        `json:"latency_ns"`
	ConsecutiveFailures uint32               `json:"consecutive_failures"`
	LastSuccess         *time.Time           `json:"last_success,omitempty"`
	LastFailure         *time.Time           `json:"last_failure,omitempty"`
}

type PoolHealth struct {
	PoolID      model.ID `json:"pool_id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Total       int      `json:"total"`
	Eligible    int      `json:"eligible"`
	Unknown     int      `json:"unknown"`
	Healthy     int      `json:"healthy"`
	Degraded    int      `json:"degraded"`
	Quarantined int      `json:"quarantined"`
	HalfOpen    int      `json:"half_open"`
	Disabled    int      `json:"disabled"`
}

type HealthControl interface {
	ProxyHealth(context.Context) ([]ProxyHealth, error)
	PoolHealth(context.Context) ([]PoolHealth, error)
	CheckProxy(context.Context, model.ID, string, uint16) (ProxyHealth, error)
	CheckPool(context.Context, model.ID, string, uint16) (PoolHealth, error)
}

func (s *Server) proxyHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "HEALTH_UNAVAILABLE", "Proxy health is unavailable.")
		return
	}
	items, err := s.health.ProxyHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "HEALTH_UNAVAILABLE", "Proxy health could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) poolHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "HEALTH_UNAVAILABLE", "Pool health is unavailable.")
		return
	}
	items, err := s.health.PoolHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "HEALTH_UNAVAILABLE", "Pool health could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) proxyHealthCheck(w http.ResponseWriter, r *http.Request) {
	s.healthCheck(w, r, "proxy", strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/health/proxies/"), "/check"))
}

func (s *Server) poolHealthCheck(w http.ResponseWriter, r *http.Request) {
	s.healthCheck(w, r, "pool", strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/health/pools/"), "/check"))
}

func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request, kind, rawID string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
		id := model.ID(rawID)
		if !id.Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_HEALTH_TARGET", "Health target ID was not accepted.")
			return
		}
		if s.health == nil {
			writeError(w, http.StatusServiceUnavailable, "HEALTH_UNAVAILABLE", "Health checks are unavailable.")
			return
		}
		var input struct {
			Host string `json:"target_host"`
			Port int    `json:"target_port"`
		}
		if !decode(w, r, &input) {
			return
		}
		input.Host = strings.TrimSpace(input.Host)
		if !proxy.ValidHost(input.Host) || input.Port < 1 || input.Port > 65535 {
			writeError(w, http.StatusBadRequest, "INVALID_HEALTH_TARGET", "Health check target was not accepted.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var result any
		var err error
		if kind == "proxy" {
			result, err = s.health.CheckProxy(ctx, id, input.Host, uint16(input.Port))
		} else {
			result, err = s.health.CheckPool(ctx, id, input.Host, uint16(input.Port))
		}
		switch {
		case err == nil:
			s.record(r.Context(), user, "health."+kind+"_checked", kind, string(id))
			writeJSON(w, http.StatusOK, map[string]any{"result": result})
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "HEALTH_TARGET_NOT_FOUND", "The active health target was not found.")
		case errors.Is(err, ErrHealthBusy):
			writeError(w, http.StatusConflict, "HEALTH_CHECK_BUSY", "A health check is already in progress.")
		case errors.Is(err, ErrHealthTargetDenied):
			writeError(w, http.StatusForbidden, "HEALTH_TARGET_DENIED", "The health check target is not allowed.")
		default:
			writeError(w, http.StatusServiceUnavailable, "HEALTH_UNAVAILABLE", "The health check could not be completed.")
		}
	})
}
