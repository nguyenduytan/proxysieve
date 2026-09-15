package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type ChainTestResult struct {
	Status        string    `json:"status"`
	TestedAt      time.Time `json:"tested_at"`
	Latency       int64     `json:"latency_ns"`
	FailureReason string    `json:"failure_reason,omitempty"`
	FailedHop     int       `json:"failed_hop,omitempty"`
	PoolID        model.ID  `json:"pool_id,omitempty"`
	ProxyID       model.ID  `json:"proxy_id,omitempty"`
}

type ChainTester func(context.Context, model.ID, string, uint16) ChainTestResult

type chainHealthRecord struct {
	Revision int64
	Result   ChainTestResult
}

var (
	errChainPoolMissing        = errors.New("chain pool is missing")
	errChainEndpointOverlap    = errors.New("chain endpoint sets overlap")
	errChainSessionUnsupported = errors.New("chain session policy is unsupported")
)

func (s *Server) chainsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listChains(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.createChain(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) chainByID(w http.ResponseWriter, r *http.Request) {
	id := model.ID(strings.TrimPrefix(r.URL.Path, "/api/v1/chains/"))
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_CHAIN", "Chain ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getChain(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updateChain(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deleteChain(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) chainTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) {
		if s.chains == nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
			return
		}
		id := model.ID(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/chains/"), "/test"))
		if !id.Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_CHAIN", "Chain ID was not accepted.")
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
			writeError(w, http.StatusBadRequest, "INVALID_CHAIN_TARGET", "Chain test target was not accepted.")
			return
		}
		record, err := s.chains.GetChain(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CHAIN_NOT_FOUND", "The chain was not found.")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain metadata could not be read.")
			return
		}
		if !record.Chain.Enabled {
			writeError(w, http.StatusConflict, "CHAIN_DISABLED", "Enable and activate this chain before testing it.")
			return
		}
		active, _ := s.chainActivation(record)
		if !active {
			writeError(w, http.StatusConflict, "CHAIN_NOT_ACTIVE", "Activate this exact chain revision before testing it.")
			return
		}
		if s.chainTester == nil {
			writeError(w, http.StatusServiceUnavailable, "CHAIN_TEST_UNAVAILABLE", "Chain testing is unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result := s.chainTester(ctx, id, input.Host, uint16(input.Port))
		result.TestedAt = s.now()
		if result.Latency < 0 {
			result.Latency = 0
		}
		if result.Status != "healthy" {
			result.Status = "unhealthy"
			if result.FailureReason != "route_unavailable" && result.FailureReason != "connect_failed" {
				result.FailureReason = "connect_failed"
			}
		} else {
			result.FailureReason = ""
			result.FailedHop = 0
			result.PoolID = ""
			result.ProxyID = ""
		}
		if !result.PoolID.Valid() {
			result.PoolID = ""
		}
		if !result.ProxyID.Valid() {
			result.ProxyID = ""
		}
		s.chainHealthMu.Lock()
		s.chainHealth[id] = chainHealthRecord{Revision: record.Revision, Result: result}
		s.chainHealthMu.Unlock()
		s.record(r.Context(), user, "chain.tested", "chain", string(id))
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
	})
}

func (s *Server) listChains(w http.ResponseWriter, r *http.Request) {
	if s.chains == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
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
	records, err := s.chains.ListChains(r.Context(), store.Page{After: model.ID(r.URL.Query().Get("after")), Limit: limit})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
		return
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, s.chainResponse(record))
	}
	next := ""
	if len(records) == limit {
		next = string(records[len(records)-1].Chain.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_after": next})
}

func (s *Server) createChain(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.chains == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
		return
	}
	var input struct {
		Chain routing.Chain `json:"chain"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Chain.ID == "" {
		input.Chain.ID = model.NewID()
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validateChain(r, input.Chain); err != nil {
		writeChainValidationError(w, err)
		return
	}
	record, err := s.chains.PutChain(r.Context(), input.Chain, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "chain.created", "chain", string(record.Chain.ID))
		writeJSON(w, http.StatusCreated, s.chainResponse(record))
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "CHAIN_EXISTS", "A chain with this ID already exists.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain metadata could not be stored.")
	}
}

func (s *Server) getChain(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.chains == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
		return
	}
	record, err := s.chains.GetChain(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, s.chainResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "CHAIN_NOT_FOUND", "The chain was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain metadata could not be read.")
	}
}

func (s *Server) updateChain(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.chains == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
		return
	}
	var input struct {
		Chain    routing.Chain `json:"chain"`
		Revision int64         `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Chain.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_CHAIN", "Chain metadata or revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validateChain(r, input.Chain); err != nil {
		writeChainValidationError(w, err)
		return
	}
	record, err := s.chains.PutChain(r.Context(), input.Chain, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "chain.updated", "chain", string(id))
		writeJSON(w, http.StatusOK, s.chainResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "CHAIN_NOT_FOUND", "The chain was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "CHAIN_CONFLICT", "The chain revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain metadata could not be stored.")
	}
}

func (s *Server) deleteChain(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.chains == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Chain revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if s.policies != nil {
		used, err := s.policyUsesChain(r, id)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy references could not be checked.")
			return
		}
		if used {
			writeError(w, http.StatusConflict, "CHAIN_IN_USE", "The chain is referenced by a saved policy.")
			return
		}
	}
	err := s.chains.DeleteChain(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "chain.deleted", "chain", string(id))
		s.chainHealthMu.Lock()
		delete(s.chainHealth, id)
		s.chainHealthMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "CHAIN_NOT_FOUND", "The chain was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "CHAIN_CONFLICT", "The chain revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain metadata could not be deleted.")
	}
}

func (s *Server) validateChain(r *http.Request, candidate routing.Chain) error {
	if candidate.Validate() != nil || s.pools == nil {
		return store.ErrInvalid
	}
	records, err := allPoolRecords(r, s.pools)
	if err != nil {
		return err
	}
	allEndpoints := map[model.ID]bool{}
	for _, hop := range candidate.Hops {
		record, ok := records[hop.PoolID]
		if !ok {
			return errChainPoolMissing
		}
		if record.Pool.SessionPolicy.Normalized().Strategy != publicsession.None {
			return errChainSessionUnsupported
		}
		for endpointID := range chainPoolEndpoints(hop.PoolID, records, map[model.ID]bool{}) {
			if allEndpoints[endpointID] {
				return errChainEndpointOverlap
			}
			allEndpoints[endpointID] = true
		}
	}
	return nil
}

func chainPoolEndpoints(id model.ID, records map[model.ID]store.PoolRecord, seen map[model.ID]bool) map[model.ID]bool {
	result := map[model.ID]bool{}
	if seen[id] {
		return result
	}
	seen[id] = true
	for _, endpointID := range records[id].Pool.EndpointIDs {
		result[endpointID] = true
	}
	for _, fallbackID := range records[id].Pool.FallbackPoolIDs {
		for endpointID := range chainPoolEndpoints(fallbackID, records, seen) {
			result[endpointID] = true
		}
	}
	return result
}

func writeChainValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errChainPoolMissing):
		writeError(w, http.StatusBadRequest, "CHAIN_POOL_NOT_FOUND", "A pool referenced by the chain was not found.")
	case errors.Is(err, errChainEndpointOverlap):
		writeError(w, http.StatusBadRequest, "CHAIN_ENDPOINT_OVERLAP", "Chain hop pools must have disjoint proxy membership, including fallbacks.")
	case errors.Is(err, errChainSessionUnsupported):
		writeError(w, http.StatusBadRequest, "CHAIN_SESSION_UNSUPPORTED", "Sticky session policies are not supported on chain hop pools.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_CHAIN", "Chain metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Chain references could not be checked.")
	}
}

func (s *Server) chainResponse(record store.ChainRecord) map[string]any {
	active, revision := s.chainActivation(record)
	response := map[string]any{"chain": record, "runtime_active": active, "activation": "staged", "runtime_revision": revision}
	if active {
		response["activation"] = "active"
		s.chainHealthMu.RLock()
		health, ok := s.chainHealth[record.Chain.ID]
		s.chainHealthMu.RUnlock()
		if ok && health.Revision == record.Revision {
			response["health"] = health.Result
		} else {
			response["health"] = map[string]string{"status": "untested"}
		}
	}
	return response
}

func (s *Server) chainActivation(record store.ChainRecord) (bool, int64) {
	active, revision := false, int64(0)
	if s.runtimeControl != nil {
		current := s.runtimeControl.CurrentRuntime()
		revision = current.Revision
		for _, chain := range current.Bundle.Chains {
			if chain.ID == record.Chain.ID && reflect.DeepEqual(chain, record.Chain) {
				active = true
				break
			}
		}
	}
	return active, revision
}

func (s *Server) chainUsesPool(r *http.Request, id model.ID) (bool, error) {
	var after model.ID
	for {
		records, err := s.chains.ListChains(r.Context(), store.Page{After: after, Limit: 1000})
		if err != nil {
			return false, err
		}
		for _, record := range records {
			for _, hop := range record.Chain.Hops {
				if hop.PoolID == id {
					return true, nil
				}
			}
		}
		if len(records) < 1000 {
			return false, nil
		}
		after = records[len(records)-1].Chain.ID
	}
}
