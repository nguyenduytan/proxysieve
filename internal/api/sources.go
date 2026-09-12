package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func (s *Server) sourcesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listSources(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.createSource(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	if s.sources == nil {
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
	records, err := s.sources.ListSources(r.Context(), store.Page{After: model.ID(r.URL.Query().Get("after")), Limit: limit})
	if errors.Is(err, store.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source metadata could not be read.")
		return
	}
	next := ""
	if len(records) == limit {
		next = string(records[len(records)-1].Source.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records, "next_after": next})
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.sources == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source storage is unavailable.")
		return
	}
	var input struct {
		Source proxy.Source `json:"source"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Source.ID == "" {
		input.Source.ID = model.NewID()
	}
	if !input.Source.LastRefreshAt.IsZero() || input.Source.LastRefreshStatus != "" || input.Source.Validate() != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SOURCE", "Source metadata was not accepted.")
		return
	}
	record, err := s.sources.PutSource(r.Context(), input.Source, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "source.created", "source", string(record.Source.ID))
		writeJSON(w, http.StatusCreated, map[string]any{"source": record})
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "SOURCE_EXISTS", "A source with this ID already exists.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source metadata could not be stored.")
	}
}

func (s *Server) sourceByID(w http.ResponseWriter, r *http.Request) {
	id := model.ID(strings.TrimPrefix(r.URL.Path, "/api/v1/sources/"))
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_SOURCE", "Source ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getSource(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updateSource(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deleteSource(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.sources == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source storage is unavailable.")
		return
	}
	record, err := s.sources.GetSource(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"source": record})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "SOURCE_NOT_FOUND", "The source was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source metadata could not be read.")
	}
}

func (s *Server) updateSource(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.sources == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source storage is unavailable.")
		return
	}
	var input struct {
		Source   proxy.Source `json:"source"`
		Revision int64        `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Source.ID != id || input.Revision < 1 || input.Source.Validate() != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SOURCE", "Source metadata or revision was not accepted.")
		return
	}
	current, err := s.sources.GetSource(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "SOURCE_NOT_FOUND", "The source was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source metadata could not be read.")
		return
	}
	input.Source.LastRefreshAt = current.Source.LastRefreshAt
	input.Source.LastRefreshStatus = current.Source.LastRefreshStatus
	record, err := s.sources.PutSource(r.Context(), input.Source, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "source.updated", "source", string(id))
		writeJSON(w, http.StatusOK, map[string]any{"source": record})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "SOURCE_NOT_FOUND", "The source was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "SOURCE_CONFLICT", "The source revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source metadata could not be stored.")
	}
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.sources == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Source revision was not accepted.")
		return
	}
	err := s.sources.DeleteSource(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "source.deleted", "source", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "SOURCE_NOT_FOUND", "The source was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "SOURCE_CONFLICT", "The source revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Source metadata could not be deleted.")
	}
}
