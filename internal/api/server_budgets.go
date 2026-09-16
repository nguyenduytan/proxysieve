package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

var (
	errBudgetClientMissing = errors.New("budget client is missing")
	errBudgetPoolMissing   = errors.New("budget pool is missing")
	errBudgetProxyMissing  = errors.New("budget proxy is missing")
)

func (s *Server) budgetsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listBudgets(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.createBudget(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) budgetByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/budgets/")
	usage := strings.HasSuffix(path, "/usage")
	if usage {
		path = strings.TrimSuffix(path, "/usage")
	}
	id := model.ID(path)
	if strings.Contains(path, "/") || !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_BUDGET", "Budget ID was not accepted.")
		return
	}
	if usage {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getBudget(w, r, id) })
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getBudget(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updateBudget(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deleteBudget(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) listBudgets(w http.ResponseWriter, r *http.Request) {
	if s.budgets == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := s.budgets.Statuses(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget usage could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getBudget(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.budgets == nil {
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget storage is unavailable.")
		return
	}
	status, err := s.budgets.Status(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, status)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "BUDGET_NOT_FOUND", "The budget was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget usage could not be read.")
	}
}

func (s *Server) createBudget(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.budgets == nil {
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget storage is unavailable.")
		return
	}
	var input struct {
		Budget publicbudget.Config `json:"budget"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validateBudget(r, input.Budget); err != nil {
		writeBudgetValidationError(w, err)
		return
	}
	record, err := s.budgets.PutConfig(r.Context(), input.Budget, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "budget.created", "budget", string(record.Budget.ID))
		status, statusErr := s.budgets.Status(r.Context(), record.Budget.ID)
		if statusErr != nil {
			writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget usage could not be read.")
			return
		}
		writeJSON(w, http.StatusCreated, status)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "BUDGET_EXISTS", "A budget with this ID already exists.")
	default:
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget metadata could not be stored.")
	}
}

func (s *Server) updateBudget(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.budgets == nil {
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget storage is unavailable.")
		return
	}
	var input struct {
		Budget   publicbudget.Config `json:"budget"`
		Revision int64               `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Budget.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_BUDGET", "Budget metadata or revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validateBudget(r, input.Budget); err != nil {
		writeBudgetValidationError(w, err)
		return
	}
	record, err := s.budgets.PutConfig(r.Context(), input.Budget, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "budget.updated", "budget", string(id))
		status, statusErr := s.budgets.Status(r.Context(), record.Budget.ID)
		if statusErr != nil {
			writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget usage could not be read.")
			return
		}
		writeJSON(w, http.StatusOK, status)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "BUDGET_NOT_FOUND", "The budget was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "BUDGET_CONFLICT", "The budget revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget metadata could not be stored.")
	}
}

func (s *Server) deleteBudget(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.budgets == nil {
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Budget revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	err := s.budgets.DeleteConfig(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "budget.deleted", "budget", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "BUDGET_NOT_FOUND", "The budget was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "BUDGET_CONFLICT", "The budget revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget metadata could not be deleted.")
	}
}

func (s *Server) validateBudget(r *http.Request, configured publicbudget.Config) error {
	if configured.Validate() != nil {
		return store.ErrInvalid
	}
	switch configured.Scope {
	case "", publicbudget.ScopeSystem:
		return nil
	case publicbudget.ScopeClient:
		if s.clients == nil {
			return store.ErrUnavailable
		}
		if _, err := s.clients.GetClient(r.Context(), configured.ScopeID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return errBudgetClientMissing
			}
			return err
		}
	case publicbudget.ScopePool:
		if s.pools == nil {
			return store.ErrUnavailable
		}
		if _, err := s.pools.GetPool(r.Context(), configured.ScopeID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return errBudgetPoolMissing
			}
			return err
		}
	case publicbudget.ScopeProxy:
		if s.endpoints == nil {
			return store.ErrUnavailable
		}
		if _, err := s.endpoints.Get(r.Context(), configured.ScopeID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return errBudgetProxyMissing
			}
			return err
		}
	}
	return nil
}

func writeBudgetValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBudgetClientMissing):
		writeError(w, http.StatusBadRequest, "BUDGET_CLIENT_NOT_FOUND", "The client referenced by the budget was not found.")
	case errors.Is(err, errBudgetPoolMissing):
		writeError(w, http.StatusBadRequest, "BUDGET_POOL_NOT_FOUND", "The pool referenced by the budget was not found.")
	case errors.Is(err, errBudgetProxyMissing):
		writeError(w, http.StatusBadRequest, "BUDGET_PROXY_NOT_FOUND", "The proxy referenced by the budget was not found.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_BUDGET", "Budget metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "BUDGETS_UNAVAILABLE", "Budget references could not be checked.")
	}
}
