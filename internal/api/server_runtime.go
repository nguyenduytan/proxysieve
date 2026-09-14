package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type RuntimeControl interface {
	CurrentRuntime() store.RuntimeRecord
	HasStagedRuntimeChanges(context.Context) (bool, error)
	ListRuntime(context.Context, int64, int) ([]store.RuntimeRecord, error)
	ActivateRuntime(context.Context, int64, model.ID) (store.RuntimeRecord, error)
	RollbackRuntime(context.Context, int64, int64, model.ID) (store.RuntimeRecord, error)
}

type runtimeState struct {
	Revision       int64    `json:"revision"`
	Source         string   `json:"source"`
	ActivatedAt    string   `json:"activated_at,omitempty"`
	ActivatedBy    model.ID `json:"activated_by,omitempty"`
	SourceRevision int64    `json:"source_revision,omitempty"`
	ProxyCount     int      `json:"proxy_count"`
	PoolCount      int      `json:"pool_count"`
	PolicyCount    int      `json:"policy_count"`
	StagedChanges  bool     `json:"staged_changes"`
}

func runtimeRepresentation(record store.RuntimeRecord) runtimeState {
	source := "inventory"
	if record.Revision == 0 {
		source = "configuration"
	} else if record.SourceRevision > 0 {
		source = "rollback"
	}
	state := runtimeState{
		Revision: record.Revision, Source: source, ActivatedBy: record.ActivatedBy,
		SourceRevision: record.SourceRevision, ProxyCount: len(record.Bundle.Proxies),
		PoolCount: len(record.Bundle.Pools), PolicyCount: len(record.Bundle.Policies),
	}
	if !record.ActivatedAt.IsZero() {
		state.ActivatedAt = record.ActivatedAt.UTC().Format(time.RFC3339Nano)
	}
	return state
}

func (s *Server) runtimeMutationRepresentation(ctx context.Context, record store.RuntimeRecord) runtimeState {
	state := runtimeRepresentation(record)
	staged, err := s.runtimeControl.HasStagedRuntimeChanges(ctx)
	if err != nil {
		// The activation already committed. Report success conservatively and let
		// the next status refresh reconcile the inventory state.
		staged = true
	}
	state.StagedChanges = staged
	return state
}

func (s *Server) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.require(w, r, auth.RoleViewer, func(_ auth.User) {
		if s.runtimeControl == nil {
			writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime activation is unavailable.")
			return
		}
		staged, err := s.runtimeControl.HasStagedRuntimeChanges(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime inventory state could not be read.")
			return
		}
		state := runtimeRepresentation(s.runtimeControl.CurrentRuntime())
		state.StagedChanges = staged
		writeJSON(w, http.StatusOK, state)
	})
}

func (s *Server) runtimeHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.require(w, r, auth.RoleViewer, func(_ auth.User) {
		if s.runtimeControl == nil {
			writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime activation is unavailable.")
			return
		}
		before, limit := int64(0), 20
		var err error
		if raw := r.URL.Query().Get("before"); raw != "" {
			before, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || before < 1 {
				writeError(w, http.StatusBadRequest, "INVALID_CURSOR", "Runtime history cursor was not accepted.")
				return
			}
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > 100 {
				writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "Runtime history limit was not accepted.")
				return
			}
		}
		records, err := s.runtimeControl.ListRuntime(r.Context(), before, limit)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime history could not be read.")
			return
		}
		items := make([]runtimeState, 0, len(records))
		for _, record := range records {
			items = append(items, runtimeRepresentation(record))
		}
		next := int64(0)
		if len(records) == limit {
			next = records[len(records)-1].Revision
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_before": next})
	})
}

func (s *Server) runtimeActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
		if s.runtimeControl == nil {
			writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime activation is unavailable.")
			return
		}
		var input struct {
			ExpectedRevision int64 `json:"expected_revision"`
		}
		if !decode(w, r, &input) || input.ExpectedRevision < 0 {
			if input.ExpectedRevision < 0 {
				writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Runtime revision was not accepted.")
			}
			return
		}
		record, err := s.runtimeControl.ActivateRuntime(r.Context(), input.ExpectedRevision, user.ID)
		if writeRuntimeMutationError(w, err) {
			return
		}
		s.record(r.Context(), user, "runtime.activated", "runtime", strconv.FormatInt(record.Revision, 10))
		writeJSON(w, http.StatusOK, s.runtimeMutationRepresentation(r.Context(), record))
	})
}

func (s *Server) runtimeRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
		if s.runtimeControl == nil {
			writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime activation is unavailable.")
			return
		}
		var input struct {
			ExpectedRevision int64 `json:"expected_revision"`
			TargetRevision   int64 `json:"target_revision"`
		}
		if !decode(w, r, &input) || input.ExpectedRevision < 1 || input.TargetRevision < 1 {
			if input.ExpectedRevision < 1 || input.TargetRevision < 1 {
				writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Runtime revisions were not accepted.")
			}
			return
		}
		record, err := s.runtimeControl.RollbackRuntime(r.Context(), input.ExpectedRevision, input.TargetRevision, user.ID)
		if writeRuntimeMutationError(w, err) {
			return
		}
		s.record(r.Context(), user, "runtime.rolled_back", "runtime", strconv.FormatInt(record.Revision, 10))
		writeJSON(w, http.StatusOK, s.runtimeMutationRepresentation(r.Context(), record))
	})
}

func writeRuntimeMutationError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, config.ErrInvalid), errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "RUNTIME_INVALID", "Saved inventory cannot form a complete runtime configuration.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "RUNTIME_CONFLICT", "The runtime revision is stale. Refresh before retrying.")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "RUNTIME_REVISION_NOT_FOUND", "The requested runtime revision was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "Runtime activation could not be completed.")
	}
	return true
}
