package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type userInput struct {
	Username string    `json:"username"`
	Password string    `json:"password"`
	Role     auth.Role `json:"role"`
	Enabled  *bool     `json:"enabled"`
}

func (s *Server) usersCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleAdmin, func(_ auth.User) {
			users, err := s.admin.ListUsers(r.Context())
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "USERS_UNAVAILABLE", "User accounts could not be read.")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": users})
		})
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleAdmin, func(actor auth.User) {
			var input userInput
			if !decode(w, r, &input) {
				return
			}
			if input.Enabled == nil || input.Password == "" {
				writeError(w, http.StatusBadRequest, "INVALID_USER", "User account metadata was not accepted.")
				return
			}
			user, err := s.admin.CreateUser(r.Context(), input.Username, input.Password, input.Role, *input.Enabled)
			switch {
			case err == nil:
				s.record(r.Context(), actor, "user.created", "user", string(user.ID))
				writeJSON(w, http.StatusCreated, user)
			case errors.Is(err, store.ErrConflict):
				writeError(w, http.StatusConflict, "USER_EXISTS", "A user with this username already exists.")
			case errors.Is(err, admin.ErrInvalidCredentials):
				writeError(w, http.StatusBadRequest, "INVALID_USER", "User account metadata or password was not accepted.")
			default:
				writeError(w, http.StatusServiceUnavailable, "USERS_UNAVAILABLE", "User account could not be created.")
			}
		})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) userByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	id := model.ID(path)
	if strings.Contains(path, "/") || !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "User ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleAdmin, func(actor auth.User) {
			var input userInput
			if !decode(w, r, &input) {
				return
			}
			if input.Enabled == nil {
				writeError(w, http.StatusBadRequest, "INVALID_USER", "User account metadata was not accepted.")
				return
			}
			user, err := s.admin.UpdateUser(r.Context(), id, input.Username, input.Password, input.Role, *input.Enabled)
			switch {
			case err == nil:
				s.record(r.Context(), actor, "user.updated", "user", string(id))
				writeJSON(w, http.StatusOK, user)
			case errors.Is(err, store.ErrNotFound):
				writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "The user was not found.")
			case errors.Is(err, store.ErrConflict):
				writeError(w, http.StatusConflict, "USER_CONFLICT", "The username is already used or the last enabled admin cannot be changed.")
			case errors.Is(err, admin.ErrInvalidCredentials):
				writeError(w, http.StatusBadRequest, "INVALID_USER", "User account metadata or password was not accepted.")
			default:
				writeError(w, http.StatusServiceUnavailable, "USERS_UNAVAILABLE", "User account could not be updated.")
			}
		})
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleAdmin, func(actor auth.User) {
			if actor.ID == id {
				writeError(w, http.StatusConflict, "USER_SELF_DELETE", "The current user cannot delete its own account.")
				return
			}
			err := s.admin.DeleteUser(r.Context(), id)
			switch {
			case err == nil:
				s.record(r.Context(), actor, "user.deleted", "user", string(id))
				w.WriteHeader(http.StatusNoContent)
			case errors.Is(err, store.ErrNotFound):
				writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "The user was not found.")
			case errors.Is(err, store.ErrConflict):
				writeError(w, http.StatusConflict, "LAST_ADMIN", "The last enabled admin cannot be deleted.")
			default:
				writeError(w, http.StatusServiceUnavailable, "USERS_UNAVAILABLE", "User account could not be deleted.")
			}
		})
	default:
		methodNotAllowed(w)
	}
}
