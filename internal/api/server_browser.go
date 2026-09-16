package api

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/downstreamauth"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	publictraffic "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

const browserPolicyTTL = 5 * time.Minute
const maxBrowserBlockReports = 100

type browserPolicySnapshot struct {
	Version   int             `json:"version"`
	Revision  int64           `json:"revision"`
	ExpiresAt time.Time       `json:"expires_at"`
	ClientID  model.ID        `json:"client_id"`
	Policies  []policy.Policy `json:"policies"`
}

type browserBlockReport struct {
	Host           string              `json:"host"`
	ResourceType   string              `json:"resource_type"`
	SessionID      model.ID            `json:"session_id"`
	PolicyID       model.ID            `json:"policy_id"`
	RuleID         model.ID            `json:"rule_id"`
	EstimatedBytes publictraffic.Bytes `json:"estimated_bytes"`
}

func (s *Server) browserPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	client, ok := s.authenticateBrowser(w, r)
	if !ok {
		return
	}
	if s.runtimeControl == nil {
		writeError(w, http.StatusServiceUnavailable, "RUNTIME_UNAVAILABLE", "The active browser policy is unavailable.")
		return
	}
	runtime := s.runtimeControl.CurrentRuntime()
	writeJSON(w, http.StatusOK, browserPolicySnapshot{
		Version: 1, Revision: runtime.Revision, ExpiresAt: s.now().Add(browserPolicyTTL),
		ClientID: client.ID, Policies: browserPolicies(runtime.Bundle.Policies, client.PolicyIDs),
	})
}

func (s *Server) browserBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	client, ok := s.authenticateBrowser(w, r)
	if !ok {
		return
	}
	if s.runtimeControl == nil || s.browserRecorder == nil {
		writeError(w, http.StatusServiceUnavailable, "BROWSER_REPORTING_UNAVAILABLE", "Browser block reporting is unavailable.")
		return
	}
	var input struct {
		Version int                  `json:"version"`
		Blocks  []browserBlockReport `json:"blocks"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Version != 1 || len(input.Blocks) < 1 || len(input.Blocks) > maxBrowserBlockReports {
		writeError(w, http.StatusBadRequest, "INVALID_BLOCK_REPORT", "Browser block reports were not accepted.")
		return
	}
	active := browserPolicies(s.runtimeControl.CurrentRuntime().Bundle.Policies, client.PolicyIDs)
	if !validBrowserBlockReports(input.Blocks, active) {
		writeError(w, http.StatusBadRequest, "INVALID_BLOCK_REPORT", "Browser block reports were not accepted.")
		return
	}
	failures := 0
	for _, block := range input.Blocks {
		event := publictraffic.Event{
			At: s.now(), RequestID: model.NewID(), ConnectionID: block.SessionID,
			ClientID: client.ID, PolicyID: block.PolicyID, RuleID: block.RuleID,
			Host: block.Host, Protocol: "browser", Action: "block", EstimatedAvoided: block.EstimatedBytes,
		}
		if err := s.browserRecorder.Record(r.Context(), event); err != nil {
			failures++
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": len(input.Blocks), "recording_failures": failures})
}

func (s *Server) authenticateBrowser(w http.ResponseWriter, r *http.Request) (auth.Client, bool) {
	if s.browserAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "CLIENT_AUTH_UNAVAILABLE", "Browser client authentication is unavailable.")
		return auth.Client{}, false
	}
	clientID, err := downstreamauth.AuthenticateBearer(r.Context(), r.Header.Get("Authorization"), s.browserAuth)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_API_KEY", "A valid client API key is required.")
		return auth.Client{}, false
	}
	client, err := s.browserAuth.FindClientAuth(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_API_KEY", "A valid client API key is required.")
		return auth.Client{}, false
	}
	return client, true
}

func browserPolicies(active []policy.Policy, allowed []model.ID) []policy.Policy {
	if len(allowed) == 0 {
		result := make([]policy.Policy, len(active))
		for i := range active {
			result[i] = active[i].Clone()
		}
		return result
	}
	allow := make(map[model.ID]bool, len(allowed))
	for _, id := range allowed {
		allow[id] = true
	}
	result := make([]policy.Policy, 0, len(active))
	for _, document := range active {
		if allow[document.ID] {
			result = append(result, document.Clone())
		}
	}
	return result
}

func validBrowserBlockReports(blocks []browserBlockReport, policies []policy.Policy) bool {
	rules := make(map[model.ID]map[model.ID]bool, len(policies))
	for _, document := range policies {
		documentRules := make(map[model.ID]bool, len(document.Rules))
		for _, rule := range document.Rules {
			documentRules[rule.ID] = browserBlockRule(rule)
		}
		rules[document.ID] = documentRules
	}
	for _, block := range blocks {
		if !proxy.ValidHost(block.Host) || !block.SessionID.Valid() || !validBrowserResourceType(block.ResourceType) || block.EstimatedBytes > publictraffic.Bytes(math.MaxInt64) {
			return false
		}
		if block.PolicyID == "" {
			if block.RuleID != "" {
				return false
			}
			continue
		}
		documentRules, exists := rules[block.PolicyID]
		if !exists || !block.RuleID.Valid() || !documentRules[block.RuleID] {
			return false
		}
	}
	return true
}

func browserBlockRule(rule policy.Rule) bool {
	if !rule.Enabled {
		return false
	}
	for _, action := range rule.Actions {
		switch action.Type {
		case "block":
			return true
		case "reject", "direct", "proxy", "chain", "mock", "redirect":
			return false
		}
	}
	return false
}

func validBrowserResourceType(value string) bool {
	if value == "" || len(value) > 32 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	switch value {
	case "document", "stylesheet", "image", "media", "font", "script", "xhr", "fetch", "websocket", "manifest", "other", "eventsource", "texttrack", "ping", "csp_report", "preflight":
		return true
	}
	return false
}
