package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const maxShadowConfigs = 64

func (s *Server) shadowCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listShadows(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.createShadow(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) shadowByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/shadow/")
	comparison := strings.HasSuffix(path, "/comparison")
	if comparison {
		path = strings.TrimSuffix(path, "/comparison")
	}
	id := model.ID(path)
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_SHADOW", "Shadow policy ID was not accepted.")
		return
	}
	if comparison {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getShadowComparison(w, id) })
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getShadow(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updateShadow(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deleteShadow(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) listShadows(w http.ResponseWriter, r *http.Request) {
	if s.shadows == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy storage is unavailable.")
		return
	}
	records, err := s.shadows.ListShadows(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policies could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records})
}

func (s *Server) createShadow(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.shadows == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy storage is unavailable.")
		return
	}
	var input struct {
		Shadow publicshadow.Config `json:"shadow"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Shadow.ID == "" {
		input.Shadow.ID = model.NewID()
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validateShadow(r, input.Shadow); err != nil {
		writeShadowValidationError(w, err)
		return
	}
	records, err := s.shadows.ListShadows(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policies could not be read.")
		return
	}
	if len(records) >= maxShadowConfigs {
		writeError(w, http.StatusConflict, "SHADOW_LIMIT", "The shadow policy limit has been reached.")
		return
	}
	record, err := s.shadows.PutShadow(r.Context(), input.Shadow, 0)
	switch {
	case err == nil:
		if s.shadowControl != nil {
			s.shadowControl.Upsert(record.Shadow)
		}
		s.record(r.Context(), user, "shadow.created", "shadow", string(record.Shadow.ID))
		writeJSON(w, http.StatusCreated, record)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "SHADOW_EXISTS", "A shadow policy with this ID already exists.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_SHADOW", "Shadow policy metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy could not be stored.")
	}
}

func (s *Server) getShadow(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.shadows == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy storage is unavailable.")
		return
	}
	record, err := s.shadows.GetShadow(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, record)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "SHADOW_NOT_FOUND", "The shadow policy was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy could not be read.")
	}
}

func (s *Server) updateShadow(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.shadows == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy storage is unavailable.")
		return
	}
	var input struct {
		Shadow   publicshadow.Config `json:"shadow"`
		Revision int64               `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Shadow.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_SHADOW", "Shadow policy metadata or revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validateShadow(r, input.Shadow); err != nil {
		writeShadowValidationError(w, err)
		return
	}
	record, err := s.shadows.PutShadow(r.Context(), input.Shadow, input.Revision)
	switch {
	case err == nil:
		if s.shadowControl != nil {
			s.shadowControl.Upsert(record.Shadow)
		}
		s.record(r.Context(), user, "shadow.updated", "shadow", string(id))
		writeJSON(w, http.StatusOK, record)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "SHADOW_NOT_FOUND", "The shadow policy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "SHADOW_CONFLICT", "The shadow policy revision is stale.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_SHADOW", "Shadow policy metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy could not be stored.")
	}
}

func (s *Server) deleteShadow(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.shadows == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Shadow policy revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	err := s.shadows.DeleteShadow(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		if s.shadowControl != nil {
			s.shadowControl.Delete(id)
		}
		s.record(r.Context(), user, "shadow.deleted", "shadow", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "SHADOW_NOT_FOUND", "The shadow policy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "SHADOW_CONFLICT", "The shadow policy revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy could not be deleted.")
	}
}

func (s *Server) getShadowComparison(w http.ResponseWriter, id model.ID) {
	if s.shadowControl == nil {
		writeError(w, http.StatusServiceUnavailable, "SHADOW_UNAVAILABLE", "Shadow comparison is unavailable.")
		return
	}
	comparison, ok := s.shadowControl.Comparison(id)
	if !ok {
		writeError(w, http.StatusNotFound, "SHADOW_NOT_FOUND", "The shadow policy was not found.")
		return
	}
	writeJSON(w, http.StatusOK, comparison)
}

func (s *Server) validateShadow(r *http.Request, config publicshadow.Config) error {
	if config.Validate() != nil {
		return store.ErrInvalid
	}
	if s.policies == nil {
		return store.ErrUnavailable
	}
	if _, err := s.policies.GetPolicy(r.Context(), config.ActivePolicyID); err != nil {
		return err
	}
	return s.validatePolicyReferences(r, config.Policy)
}

func writeShadowValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_SHADOW", "Shadow policy metadata was not accepted.")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusBadRequest, "ACTIVE_POLICY_NOT_FOUND", "The active policy referenced by the shadow policy was not found.")
	case errors.Is(err, errPolicyPoolMissing):
		writeError(w, http.StatusBadRequest, "POLICY_POOL_NOT_FOUND", "A pool referenced by the shadow policy was not found.")
	case errors.Is(err, errPolicyChainMissing):
		writeError(w, http.StatusBadRequest, "POLICY_CHAIN_NOT_FOUND", "A chain referenced by the shadow policy was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy references could not be checked.")
	}
}

func (s *Server) shadowUsesPolicy(ctx context.Context, id model.ID) (bool, error) {
	if s.shadows == nil {
		return false, nil
	}
	records, err := s.shadows.ListShadows(ctx)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.Shadow.ActivePolicyID == id {
			return true, nil
		}
	}
	return false, nil
}
