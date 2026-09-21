package api

import (
	"errors"
	"net/http"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

var errPolicyPoolMissing = errors.New("policy pool is missing")
var errPolicyChainMissing = errors.New("policy chain is missing")

func (s *Server) policiesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.listPolicies(w, r) })
	case http.MethodPost:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.createPolicy(w, r, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) policyByID(w http.ResponseWriter, r *http.Request) {
	id := model.ID(strings.TrimPrefix(r.URL.Path, "/api/v1/policies/"))
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_POLICY", "Policy ID was not accepted.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.require(w, r, auth.RoleViewer, func(_ auth.User) { s.getPolicy(w, r, id) })
	case http.MethodPatch:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.updatePolicy(w, r, id, user) })
	case http.MethodDelete:
		s.requireMutation(w, r, auth.RoleOperator, func(user auth.User) { s.deletePolicy(w, r, id, user) })
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request) {
	if s.policies == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
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
	records, err := s.policies.ListPolicies(r.Context(), page)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
		return
	}
	next := ""
	if len(records) == limit {
		next = string(records[len(records)-1].Policy.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records, "next_after": next})
}

func (s *Server) createPolicy(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.policies == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
		return
	}
	var input struct {
		Policy policy.Policy `json:"policy"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Policy.ID == "" {
		input.Policy.ID = model.NewID()
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validatePolicyReferences(r, input.Policy); err != nil {
		writePolicyValidationError(w, err)
		return
	}
	record, err := s.policies.PutPolicy(r.Context(), input.Policy, 0)
	switch {
	case err == nil:
		s.record(r.Context(), user, "policy.created", "policy", string(record.Policy.ID))
		writeJSON(w, http.StatusCreated, s.policyResponse(record))
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "POLICY_EXISTS", "A policy with this ID already exists.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_POLICY", "Policy metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy metadata could not be stored.")
	}
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request, id model.ID) {
	if s.policies == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
		return
	}
	record, err := s.policies.GetPolicy(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, s.policyResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "The policy was not found.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy metadata could not be read.")
	}
}

func (s *Server) updatePolicy(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.policies == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
		return
	}
	var input struct {
		Policy   policy.Policy `json:"policy"`
		Revision int64         `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Policy.ID != id || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_POLICY", "Policy metadata or revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if err := s.validatePolicyReferences(r, input.Policy); err != nil {
		writePolicyValidationError(w, err)
		return
	}
	record, err := s.policies.PutPolicy(r.Context(), input.Policy, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "policy.updated", "policy", string(id))
		writeJSON(w, http.StatusOK, s.policyResponse(record))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "The policy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "POLICY_CONFLICT", "The policy revision is stale.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_POLICY", "Policy metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy metadata could not be stored.")
	}
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request, id model.ID, user auth.User) {
	if s.policies == nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_REVISION", "Policy revision was not accepted.")
		return
	}
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if referenced, err := s.shadowUsesPolicy(r.Context(), id); err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Shadow policy references could not be checked.")
		return
	} else if referenced {
		writeError(w, http.StatusConflict, "POLICY_IN_USE", "The policy is referenced by a shadow configuration.")
		return
	}
	err := s.policies.DeletePolicy(r.Context(), id, input.Revision)
	switch {
	case err == nil:
		s.record(r.Context(), user, "policy.deleted", "policy", string(id))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "The policy was not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "POLICY_CONFLICT", "The policy revision is stale.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy metadata could not be deleted.")
	}
}

func (s *Server) policyResponse(record store.PolicyRecord) map[string]any {
	active, revision := s.policyRuntimeState(record.Policy)
	activation := "staged"
	if active {
		activation = "active"
	}
	return map[string]any{"policy": record, "runtime_active": active, "activation": activation, "runtime_revision": revision}
}

func (s *Server) policyRuntimeState(candidate policy.Policy) (bool, int64) {
	active, revision := false, int64(0)
	if s.runtimeControl != nil {
		current := s.runtimeControl.CurrentRuntime()
		revision = current.Revision
		for _, document := range current.Bundle.Policies {
			if document.ID == candidate.ID && reflect.DeepEqual(document, candidate) {
				active = true
				break
			}
		}
	}
	return active, revision
}

func (s *Server) validatePolicyReferences(r *http.Request, document policy.Policy) error {
	if document.Validate() != nil {
		return store.ErrInvalid
	}
	for _, rule := range document.Rules {
		for _, action := range rule.Actions {
			switch action.Type {
			case "proxy":
				if s.pools == nil {
					return store.ErrUnavailable
				}
				if _, err := s.pools.GetPool(r.Context(), action.PoolID); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return errPolicyPoolMissing
					}
					return err
				}
			case "chain":
				if s.chains == nil {
					return store.ErrUnavailable
				}
				for _, id := range append([]model.ID{action.ChainID}, action.FallbackChainIDs...) {
					if _, err := s.chains.GetChain(r.Context(), id); err != nil {
						if errors.Is(err, store.ErrNotFound) {
							return errPolicyChainMissing
						}
						return err
					}
				}
			}
		}
	}
	return nil
}

func writePolicyValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPolicyPoolMissing):
		writeError(w, http.StatusBadRequest, "POLICY_POOL_NOT_FOUND", "A pool referenced by the policy was not found.")
	case errors.Is(err, errPolicyChainMissing):
		writeError(w, http.StatusBadRequest, "POLICY_CHAIN_NOT_FOUND", "A chain referenced by the policy was not found.")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_POLICY", "Policy metadata was not accepted.")
	default:
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy references could not be checked.")
	}
}

func (s *Server) policyUsesPool(r *http.Request, id model.ID) (bool, error) {
	records, err := allPolicyRecords(r, s.policies)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		for _, rule := range record.Policy.Rules {
			for _, action := range rule.Actions {
				if action.Type == "proxy" && action.PoolID == id {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (s *Server) policyUsesChain(r *http.Request, id model.ID) (bool, error) {
	records, err := allPolicyRecords(r, s.policies)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		for _, rule := range record.Policy.Rules {
			for _, action := range rule.Actions {
				if action.Type == "chain" {
					for _, referenced := range append([]model.ID{action.ChainID}, action.FallbackChainIDs...) {
						if referenced == id {
							return true, nil
						}
					}
				}
			}
		}
	}
	return false, nil
}

func allPolicyRecords(r *http.Request, policies store.Policies) (map[model.ID]store.PolicyRecord, error) {
	const pageSize = 1000
	records := map[model.ID]store.PolicyRecord{}
	after := model.ID("")
	for {
		page, err := policies.ListPolicies(r.Context(), store.Page{After: after, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		for _, record := range page {
			records[record.Policy.ID] = record
		}
		if len(page) < pageSize {
			return records, nil
		}
		after = page[len(page)-1].Policy.ID
	}
}

type policySimulationRequest struct {
	Listener      string    `json:"listener"`
	ClientID      model.ID  `json:"client_id"`
	Protocol      string    `json:"protocol"`
	Scheme        string    `json:"scheme"`
	Host          string    `json:"host"`
	Port          uint16    `json:"port"`
	Method        *string   `json:"method"`
	Path          *string   `json:"path"`
	DestinationIP string    `json:"destination_ip"`
	Timestamp     time.Time `json:"timestamp"`
}

type policySimulationCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
}

type policySimulationRule struct {
	RuleID     model.ID                    `json:"rule_id"`
	Matched    bool                        `json:"matched"`
	Conditions []policySimulationCondition `json:"conditions"`
}

type policySimulationResponse struct {
	PolicyID       model.ID               `json:"policy_id"`
	Revision       int64                  `json:"revision"`
	Outcome        string                 `json:"outcome"`
	Actions        []policy.Action        `json:"actions"`
	MatchedRuleIDs []model.ID             `json:"matched_rule_ids"`
	Rules          []policySimulationRule `json:"rules"`
	UnknownFields  []string               `json:"unknown_fields"`
	SimulationOnly bool                   `json:"simulation_only"`
	RuntimeActive  bool                   `json:"runtime_active"`
}

func (s *Server) simulatePolicy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/policies/"), "/simulate")
	id := model.ID(path)
	if !id.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_POLICY", "Policy ID was not accepted.")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.requireMutation(w, r, auth.RoleViewer, func(_ auth.User) {
		if s.policies == nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy storage is unavailable.")
			return
		}
		var input policySimulationRequest
		if !decode(w, r, &input) {
			return
		}
		request, visibility, ok := s.policySimulationContext(input)
		if !ok {
			writeError(w, http.StatusBadRequest, "INVALID_SIMULATION", "Simulation request fields were not accepted.")
			return
		}
		record, err := s.policies.GetPolicy(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "The policy was not found.")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "Policy metadata could not be read.")
			return
		}
		result, evaluateErr := policy.Evaluate(record.Policy, request, visibility, true)
		if evaluateErr != nil && !errors.Is(evaluateErr, policy.ErrNoRoute) {
			writeError(w, http.StatusServiceUnavailable, "SIMULATION_UNAVAILABLE", "The policy could not be simulated.")
			return
		}
		response := representPolicySimulation(record, result)
		response.RuntimeActive, _ = s.policyRuntimeState(record.Policy)
		if errors.Is(evaluateErr, policy.ErrNoRoute) {
			response.Outcome = "no_route"
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func (s *Server) policySimulationContext(input policySimulationRequest) (policy.RequestContext, policy.Visibility, bool) {
	if !model.ID(input.Listener).Valid() || input.ClientID != "" && !input.ClientID.Valid() || !proxy.ValidHost(input.Host) || input.Port == 0 {
		return policy.RequestContext{}, policy.Visibility{}, false
	}
	switch input.Protocol {
	case "http", "connect", "socks5":
	default:
		return policy.RequestContext{}, policy.Visibility{}, false
	}
	if input.Scheme != "" && input.Scheme != "http" && input.Scheme != "https" {
		return policy.RequestContext{}, policy.Visibility{}, false
	}
	if input.Method != nil && (*input.Method == "" || len(*input.Method) > 32) || input.Path != nil && len(*input.Path) > 4096 {
		return policy.RequestContext{}, policy.Visibility{}, false
	}
	var destination netip.Addr
	if input.DestinationIP != "" {
		var err error
		destination, err = netip.ParseAddr(input.DestinationIP)
		if err != nil || destination.Zone() != "" {
			return policy.RequestContext{}, policy.Visibility{}, false
		}
	}
	if input.Timestamp.IsZero() {
		input.Timestamp = s.now()
	}
	request := policy.RequestContext{
		RequestID: "simulation", ClientID: input.ClientID, Listener: input.Listener,
		Protocol: input.Protocol, Scheme: input.Scheme, Host: input.Host, Port: input.Port,
		DestinationIP: destination, Timestamp: input.Timestamp.UTC(),
	}
	visibility := policy.Visibility{Host: true}
	if input.Method != nil {
		request.Method = model.Optional[string]{Known: true, Value: *input.Method}
		visibility.Method = true
	}
	if input.Path != nil {
		request.Path = model.Optional[string]{Known: true, Value: *input.Path}
		visibility.Path = true
	}
	return request, visibility, true
}

func representPolicySimulation(record store.PolicyRecord, result policy.Result) policySimulationResponse {
	response := policySimulationResponse{
		PolicyID: record.Policy.ID, Revision: record.Revision, Outcome: "no_route",
		Actions:        make([]policy.Action, 0, len(result.Actions)),
		MatchedRuleIDs: make([]model.ID, 0, len(result.MatchedRuleIDs)),
		UnknownFields:  make([]string, 0, len(result.Trace.UnknownFields)),
		SimulationOnly: true,
	}
	response.Actions = append(response.Actions, result.Actions...)
	response.MatchedRuleIDs = append(response.MatchedRuleIDs, result.MatchedRuleIDs...)
	response.UnknownFields = append(response.UnknownFields, result.Trace.UnknownFields...)
terminalAction:
	for _, action := range result.Actions {
		switch action.Type {
		case "block", "reject", "proxy", "chain", "direct", "mock", "redirect":
			response.Outcome = action.Type
			break terminalAction
		}
	}
	for _, rule := range result.Trace.Rules {
		represented := policySimulationRule{RuleID: rule.RuleID, Matched: rule.Matched}
		for _, condition := range rule.Conditions {
			represented.Conditions = append(represented.Conditions, policySimulationCondition{
				Field: condition.Field, Operator: condition.Operator,
				State: matchStateName(condition.State), Reason: condition.Reason,
			})
		}
		response.Rules = append(response.Rules, represented)
	}
	return response
}

func matchStateName(state policy.MatchState) string {
	switch state {
	case policy.Match:
		return "match"
	case policy.NoMatch:
		return "no_match"
	default:
		return "unknown"
	}
}
