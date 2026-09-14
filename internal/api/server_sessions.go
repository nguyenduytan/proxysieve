package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nguyenduytan/proxysieve/internal/session"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
)

func (s *Server) sessionsCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.require(w, r, auth.RoleViewer, func(_ auth.User) {
		if !s.sessionStoreAvailable(w) {
			return
		}
		items, err := s.sessions.List(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SESSIONS_UNAVAILABLE", "Runtime sessions could not be read.")
			return
		}
		if len(items) > 1000 {
			items = items[:1000]
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
}

func (s *Server) sessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	rotate := strings.HasSuffix(path, "/rotate")
	idRaw := strings.TrimSuffix(path, "/rotate")
	id := model.ID(idRaw)
	if !id.Valid() || idRaw == "" || strings.Contains(idRaw, "/") {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "The session was not found.")
		return
	}
	if rotate {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
			if !s.sessionStoreAvailable(w) {
				return
			}
			if err := s.sessions.Rotate(r.Context(), id, publicsession.Manual); err != nil {
				s.writeSessionError(w, err)
				return
			}
			s.record(r.Context(), user, "session.rotated", "session", idRaw)
			entry, err := s.sessions.Get(r.Context(), id)
			if err != nil {
				s.writeSessionError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, entry)
		})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) {
			if !s.sessionStoreAvailable(w) {
				return
			}
			entry, err := s.sessions.Get(r.Context(), id)
			if err != nil {
				s.writeSessionError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, entry)
		})
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
			if !s.sessionStoreAvailable(w) {
				return
			}
			if err := s.sessions.Delete(r.Context(), id); err != nil {
				s.writeSessionError(w, err)
				return
			}
			s.record(r.Context(), user, "session.deleted", "session", idRaw)
			w.WriteHeader(http.StatusNoContent)
		})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) sessionStoreAvailable(w http.ResponseWriter) bool {
	if s.sessions != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "SESSIONS_UNAVAILABLE", "Runtime session state is unavailable.")
	return false
}

func (s *Server) writeSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "The session was not found.")
		return
	}
	if errors.Is(err, session.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "SESSION_INVALID", "The session request was not accepted.")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "SESSIONS_UNAVAILABLE", "Runtime session state is unavailable.")
}
