package api

import "net/http"

func (s *Server) budgetStatuses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
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
