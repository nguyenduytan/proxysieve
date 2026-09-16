package api

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

var (
	errPoolEndpointMissing = errors.New("pool endpoint is missing")
	errPoolFallbackMissing = errors.New("pool fallback is missing")
	errPoolCycle           = errors.New("pool fallback cycle")
)

func (s *Server) poolsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listPools(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.createPool(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) poolByID(w http.ResponseWriter, r *http.Request) {
	id := model.ID(strings.TrimPrefix(r.URL.Path, "/api/v1/pools/"))
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "Pool ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getPool(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updatePool(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deletePool(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	if s.pools == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool storage is unavailable.")
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
	page := store.Page{After: model.ID(r.URL.Query().Get("after")), Limit: limit}
	if err := page.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAGE", "Pagination values were not accepted.")
		return
	}
	records, err := s.pools.ListPools(r.Context(), page)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool storage is unavailable.")
		return
	}
	next := ""
	if len(records) == limit {
		next = string(records[len(records)-1].Pool.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records, "next_after": next})
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.pools == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool storage is unavailable.")
		return
	}
	var input struct {
		Pool routing.Pool `json:"pool"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Pool.ID == "" {
		input.Pool.ID = model.NewID()
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validatePool(r, input.Pool); err != nil {
		writePoolValidationError(w, err)
		return
	}
	record, err := s.pools.PutPool(r.Context(), input.Pool, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "pool.created", "pool", string(record.Pool.ID))
		writeJSON(w, http.StatusCreated, s.poolResponse(record))
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "POOL_EXISTS", "A pool with this ID already exists.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool metadata could not be stored.")
	}
}

func (s *Server) getPool(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.pools == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool storage is unavailable.")
		return
	}
	record, err := s.pools.GetPool(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, s.poolResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "The pool was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool metadata could not be read.")
	}
}

func (s *Server) updatePool(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.pools == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool storage is unavailable.")
		return
	}
	var input struct {
		Pool     routing.Pool `json:"pool"`
		Revision int64        `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Pool.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "Pool metadata or revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validatePool(r, input.Pool); err != nil {
		writePoolValidationError(w, err)
		return
	}
	record, err := s.pools.PutPool(r.Context(), input.Pool, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "pool.updated", "pool", string(id))
		writeJSON(w, http.StatusOK, s.poolResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "The pool was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "POOL_CONFLICT", "The pool revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool metadata could not be stored.")
	}
}

func (s *Server) deletePool(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.pools == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Pool revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if s.policies != nil {
		referenced, err := s.policyUsesPool(r, id)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy references could not be checked.")
			return
		}
		if referenced {
			writeError(w, http.StatusConflict, "POOL_IN_USE", "The pool is referenced by a saved policy.")
			return
		}
	}
	referenced, err := s.poolIsFallback(r, id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool references could not be checked.")
		return
	}
	if referenced {
		writeError(w, http.StatusConflict, "POOL_IN_USE", "The pool is used as a fallback by another pool.")
		return
	}
	if s.chains != nil {
		referenced, err = s.chainUsesPool(r, id)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain references could not be checked.")
			return
		}
		if referenced {
			writeError(w, http.StatusConflict, "POOL_IN_USE", "The pool is used by a saved proxy chain.")
			return
		}
	}
	if s.budgets != nil && s.budgets.References(publicbudget.ScopePool, id) {
		writeError(w, http.StatusConflict, "POOL_IN_USE", "The pool is referenced by a saved budget.")
		return
	}
	err = s.pools.DeletePool(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "pool.deleted", "pool", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "The pool was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "POOL_CONFLICT", "The pool revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool metadata could not be deleted.")
	}
}

func (s *Server) poolResponse(record store.PoolRecord) map[string]any {
	active, revision := false, int64(0)
	if s.runtimeControl != nil {
		current := s.runtimeControl.CurrentRuntime()
		revision = current.Revision
		for _, pool := range current.Bundle.Pools {
			if pool.ID == record.Pool.ID && reflect.DeepEqual(pool, record.Pool) {
				active = true
				break
			}
		}
	}
	activation := "staged"
	if active {
		activation = "active"
	}
	return map[string]any{"pool": record, "runtime_active": active, "activation": activation, "runtime_revision": revision}
}

func (s *Server) validatePool(r *http.Request, candidate routing.Pool) error {
	if candidate.Validate() != nil || strings.TrimSpace(candidate.Name) == "" {
		return store.ErrInvalid
	}
	if s.endpoints == nil {
		if len(candidate.EndpointIDs) > 0 {
			return store.ErrUnavailable
		}
	} else {
		for _, id := range candidate.EndpointIDs {
			if _, err := s.endpoints.Get(r.Context(), id); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return errPoolEndpointMissing
				}
				return err
			}
		}
	}
	for _, id := range candidate.FallbackPoolIDs {
		if _, err := s.pools.GetPool(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return errPoolFallbackMissing
			}
			return err
		}
	}
	records, err := allPoolRecords(r, s.pools)
	if err != nil {
		return err
	}
	records[candidate.ID] = store.PoolRecord{Pool: candidate}
	if hasPoolCycle(records) {
		return errPoolCycle
	}
	return nil
}

func allPoolRecords(r *http.Request, pools store.Pools) (map[model.ID]store.PoolRecord, error) {
	const pageSize = 1000
	records := map[model.ID]store.PoolRecord{}
	after := model.ID("")
	for {
		page, err := pools.ListPools(r.Context(), store.Page{After: after, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		for _, record := range page {
			records[record.Pool.ID] = record
		}
		if len(page) < pageSize {
			return records, nil
		}
		after = page[len(page)-1].Pool.ID
	}
}

func hasPoolCycle(records map[model.ID]store.PoolRecord) bool {
	state := map[model.ID]uint8{}
	var visit func(model.ID) bool
	visit = func(id model.ID) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, next := range records[id].Pool.FallbackPoolIDs {
			if visit(next) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range records {
		if visit(id) {
			return true
		}
	}
	return false
}

func (s *Server) poolIsFallback(r *http.Request, id model.ID) (bool, error) {
	records, err := allPoolRecords(r, s.pools)
	if err != nil {
		return false, err
	}
	for poolID, record := range records {
		if poolID == id {
			continue
		}
		for _, fallback := range record.Pool.FallbackPoolIDs {
			if fallback == id {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Server) poolUsesEndpoint(r *http.Request, id model.ID) (bool, error) {
	records, err := allPoolRecords(r, s.pools)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		for _, endpointID := range record.Pool.EndpointIDs {
			if endpointID == id {
				return true, nil
			}
		}
	}
	return false, nil
}

func writePoolValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPoolEndpointMissing):
		writeError(w, http.StatusBadRequest, "POOL_ENDPOINT_NOT_FOUND", "A proxy referenced by the pool was not found.")
	case errors.Is(err, errPoolFallbackMissing):
		writeError(w, http.StatusBadRequest, "POOL_FALLBACK_NOT_FOUND", "A fallback pool was not found.")
	case errors.Is(err, errPoolCycle):
		writeError(w, http.StatusBadRequest, "POOL_FALLBACK_CYCLE", "Fallback pools must not form a cycle.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "Pool metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Pool references could not be checked.")
	}
}
