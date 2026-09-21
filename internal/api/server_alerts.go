package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	internalalert "github.com/nguyenduytan/proxysieve/internal/alert"
	publicalert "github.com/nguyenduytan/proxysieve/pkg/alert"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func (s *Server) alertsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.listAlertRules(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.createAlertRule(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) webhooksCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.listWebhooks(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.createWebhook(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) alertByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/")
	id := model.ID(path)
	if strings.Contains(path, "/") || !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_ALERT", "Alert rule ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.getAlertRule(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.updateAlertRule(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.deleteAlertRule(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) webhookByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/webhooks/")
	test := strings.HasSuffix(path, "/test")
	deliveries := strings.HasSuffix(path, "/deliveries")
	if test {
		path = strings.TrimSuffix(path, "/test")
	}
	if deliveries {
		path = strings.TrimSuffix(path, "/deliveries")
	}
	id := model.ID(path)
	if strings.Contains(path, "/") || !id.Valid() || test && deliveries {
		writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK", "Webhook ID was not accepted.")
		return
	}
	if test {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.testWebhook(w, r, id, user) })
		return
	}
	if deliveries {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.listWebhookDeliveries(w, r, id) })
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) { s.getWebhook(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.updateWebhook(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleAdmin, func(user auth.User) { s.deleteWebhook(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	if s.alerts == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := s.alerts.ListAlertRules(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert rules could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.alerts == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "delivery": internalalert.Stats{}})
		return
	}
	items, err := s.alerts.ListWebhooks(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhooks could not be read.")
		return
	}
	stats := internalalert.Stats{}
	if s.alertControl != nil {
		stats = s.alertControl.Stats()
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "delivery": stats})
}

func (s *Server) getAlertRule(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert storage is unavailable.")
		return
	}
	record, err := s.alerts.GetAlertRule(r.Context(), id)
	writeAlertRecord(w, record, err)
}

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook storage is unavailable.")
		return
	}
	record, err := s.alerts.GetWebhook(r.Context(), id)
	writeWebhookRecord(w, record, err)
}

func (s *Server) createAlertRule(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert storage is unavailable.")
		return
	}
	var input struct {
		Rule publicalert.Rule `json:"rule"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if !s.validateAlertRule(w, r, input.Rule) {
		return
	}
	record, err := s.alerts.PutAlertRule(r.Context(), input.Rule, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "alert.created", "alert", string(input.Rule.ID))
		writeJSON(w, http.StatusCreated, record)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "ALERT_EXISTS", "An alert rule with this ID already exists.")
	default:
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert rule could not be stored.")
	}
}

func (s *Server) updateAlertRule(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert storage is unavailable.")
		return
	}
	var input struct {
		Rule     publicalert.Rule `json:"rule"`
		Revision int64            `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Rule.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_ALERT", "Alert rule metadata or revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if !s.validateAlertRule(w, r, input.Rule) {
		return
	}
	record, err := s.alerts.PutAlertRule(r.Context(), input.Rule, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "alert.updated", "alert", string(id))
		writeJSON(w, http.StatusOK, record)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "ALERT_NOT_FOUND", "The alert rule was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "ALERT_CONFLICT", "The alert rule revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert rule could not be stored.")
	}
}

func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Alert rule revision was not accepted.")
		return
	}
	err := s.alerts.DeleteAlertRule(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "alert.deleted", "alert", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "ALERT_NOT_FOUND", "The alert rule was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "ALERT_CONFLICT", "The alert rule revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert rule could not be deleted.")
	}
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook storage is unavailable.")
		return
	}
	var input struct {
		Webhook publicalert.Webhook `json:"webhook"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Webhook.Validate() != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK", "Webhook metadata was not accepted. Use an HTTPS URL without credentials, query or fragment.")
		return
	}
	record, err := s.alerts.PutWebhook(r.Context(), input.Webhook, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "webhook.created", "webhook", string(input.Webhook.ID))
		writeJSON(w, http.StatusCreated, record)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "WEBHOOK_EXISTS", "A webhook with this ID already exists.")
	default:
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook could not be stored.")
	}
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook storage is unavailable.")
		return
	}
	var input struct {
		Webhook  publicalert.Webhook `json:"webhook"`
		Revision int64               `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Webhook.ID != id || input.Revision < 1 || input.Webhook.Validate() != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK", "Webhook metadata or revision was not accepted.")
		return
	}
	record, err := s.alerts.PutWebhook(r.Context(), input.Webhook, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "webhook.updated", "webhook", string(id))
		writeJSON(w, http.StatusOK, record)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "The webhook was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "WEBHOOK_CONFLICT", "The webhook revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook could not be stored.")
	}
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Webhook revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	rules, err := s.alerts.ListAlertRules(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Webhook references could not be checked.")
		return
	}
	for _, record := range rules {
		for _, webhookID := range record.Rule.WebhookIDs {
			if webhookID == id {
				writeError(w, http.StatusConflict, "WEBHOOK_IN_USE", "The webhook is referenced by an alert rule.")
				return
			}
		}
	}
	err = s.alerts.DeleteWebhook(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "webhook.deleted", "webhook", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "The webhook was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "WEBHOOK_CONFLICT", "The webhook revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook could not be deleted.")
	}
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.alertControl == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook delivery is unavailable.")
		return
	}
	delivery, err := s.alertControl.Test(r.Context(), id)
	if delivery.ID.Valid() {
		s.record(r.Context(), user, "webhook.tested", "webhook", string(id))
		writeJSON(w, http.StatusOK, map[string]any{"delivery": delivery})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "The webhook was not found.")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook test could not run.")
}

func (s *Server) listWebhookDeliveries(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook storage is unavailable.")
		return
	}
	if _, err := s.alerts.GetWebhook(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "The webhook was not found.")
		return
	} else if err != nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook could not be read.")
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
	items, err := s.alerts.ListWebhookDeliveries(r.Context(), id, limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook deliveries could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) validateAlertRule(w http.ResponseWriter, r *http.Request, rule publicalert.Rule) bool {
	if rule.Validate() != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ALERT", "Alert rule metadata was not accepted.")
		return false
	}
	for _, id := range rule.WebhookIDs {
		if _, err := s.alerts.GetWebhook(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "ALERT_WEBHOOK_NOT_FOUND", "A webhook referenced by the alert rule was not found.")
			} else {
				writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook references could not be checked.")
			}
			return false
		}
	}
	return true
}

func writeAlertRecord(w http.ResponseWriter, record store.AlertRuleRecord, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, record)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "ALERT_NOT_FOUND", "The alert rule was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "ALERTS_UNAVAILABLE", "Alert rule could not be read.")
	}
}

func writeWebhookRecord(w http.ResponseWriter, record store.WebhookRecord, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, record)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "WEBHOOK_NOT_FOUND", "The webhook was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "Webhook could not be read.")
	}
}
