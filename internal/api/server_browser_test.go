package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/admin"
	"github.com/nguyenduytan/proxysieve/internal/keys"
	"github.com/nguyenduytan/proxysieve/internal/security"
	internaltraffic "github.com/nguyenduytan/proxysieve/internal/traffic"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type browserAuthStore struct {
	hash   [32]byte
	key    auth.APIKey
	client auth.Client
}

func (s browserAuthStore) FindAPIKey(_ context.Context, hash [32]byte) (auth.APIKey, error) {
	if hash != s.hash {
		return auth.APIKey{}, store.ErrNotFound
	}
	return s.key, nil
}

func (s browserAuthStore) FindClientAuth(_ context.Context, id model.ID) (auth.Client, error) {
	if id != s.client.ID {
		return auth.Client{}, store.ErrNotFound
	}
	return s.client.Clone(), nil
}

func TestBrowserPolicyAndBlockReporting(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC)
	allowed := browserTestPolicy("allowed", "block-images")
	other := browserTestPolicy("other", "other-rule")
	client := auth.Client{ID: "browser-client", Name: "Browser", Enabled: true, AuthMethod: "api_key", PolicyIDs: []model.ID{allowed.ID}, CreatedAt: now}
	token, _, hash, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	recorder, _ := internaltraffic.NewMemory(10)
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, recorder, nil, nil)
	server.now = func() time.Time { return now }
	server.browserAuth = browserAuthStore{
		hash: hash, client: client,
		key: auth.APIKey{ID: "browser-key", ClientID: client.ID, Prefix: token[:12], CreatedAt: now},
	}
	server.SetRuntimeControl(&fakeRuntimeControl{current: store.RuntimeRecord{Revision: 7, Bundle: store.RuntimeBundle{Policies: []policy.Policy{allowed, other}}}})

	policyRequest := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/browser/policy", nil)
	policyRequest.Header.Set("Authorization", "Bearer "+token)
	policyResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusOK {
		t.Fatalf("policy status %d: %s", policyResponse.Code, policyResponse.Body.String())
	}
	var snapshot browserPolicySnapshot
	if json.Unmarshal(policyResponse.Body.Bytes(), &snapshot) != nil || snapshot.Version != 1 || snapshot.Revision != 7 || snapshot.ClientID != client.ID || !snapshot.ExpiresAt.Equal(now.Add(browserPolicyTTL)) || len(snapshot.Policies) != 1 || snapshot.Policies[0].ID != allowed.ID {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}

	body := []byte(`{"version":1,"blocks":[{"host":"assets.example.invalid","resource_type":"image","session_id":"browser-session","policy_id":"allowed","rule_id":"block-images","estimated_bytes":2048},{"host":"media.example.invalid","resource_type":"media","session_id":"browser-session","estimated_bytes":0}]}`)
	blockRequest := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/browser/blocks", bytes.NewReader(body))
	blockRequest.Header.Set("Authorization", "Bearer "+token)
	blockRequest.Header.Set("Content-Type", "application/json")
	blockResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(blockResponse, blockRequest)
	if blockResponse.Code != http.StatusAccepted {
		t.Fatalf("report status %d: %s", blockResponse.Code, blockResponse.Body.String())
	}
	events, dropped := recorder.Snapshot()
	if dropped != 0 || len(events) != 2 || events[0].ClientID != client.ID || events[0].ConnectionID != "browser-session" || events[0].PolicyID != allowed.ID || events[0].RuleID != "block-images" || events[0].Protocol != "browser" || events[0].Action != "block" || events[0].EstimatedAvoided != 2048 || events[1].PolicyID != "" || events[1].RuleID != "" || events[1].EstimatedAvoided != 0 {
		t.Fatalf("unexpected browser traffic: %+v, dropped %d", events, dropped)
	}
}

func TestBrowserControlRejectsInvalidAuthAndAttribution(t *testing.T) {
	now := time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC)
	document := browserTestPolicy("allowed", "block-images")
	document.Rules = append(document.Rules, policy.Rule{ID: "allow-api", Name: "Allow API", Enabled: true, Conditions: policy.Condition{}, Actions: []policy.Action{{Type: "allow"}, {Type: "direct"}}})
	client := auth.Client{ID: "browser-client", Name: "Browser", Enabled: true, AuthMethod: "api_key", PolicyIDs: []model.ID{document.ID}, CreatedAt: now}
	token, _, hash, _ := keys.Generate()
	recorder, _ := internaltraffic.NewMemory(10)
	service, _ := admin.New(&memoryUsers{users: map[string]userRecord{}}, security.DefaultPasswordParams())
	server, _ := New(service, recorder, nil, nil)
	server.browserAuth = browserAuthStore{hash: hash, client: client, key: auth.APIKey{ID: "browser-key", ClientID: client.ID, Prefix: token[:12], CreatedAt: now}}
	server.SetRuntimeControl(&fakeRuntimeControl{current: store.RuntimeRecord{Revision: 1, Bundle: store.RuntimeBundle{Policies: []policy.Policy{document}}}})

	unauthorized := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/browser/policy", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", unauthorizedResponse.Code)
	}

	body := []byte(`{"version":1,"blocks":[{"host":"assets.example.invalid","resource_type":"image","session_id":"browser-session","policy_id":"allowed","rule_id":"allow-api","estimated_bytes":1}]}`)
	forged := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/browser/blocks", bytes.NewReader(body))
	forged.Header.Set("Authorization", "Bearer "+token)
	forged.Header.Set("Content-Type", "application/json")
	forgedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(forgedResponse, forged)
	if forgedResponse.Code != http.StatusBadRequest {
		t.Fatalf("forged status %d: %s", forgedResponse.Code, forgedResponse.Body.String())
	}
	if events, _ := recorder.Snapshot(); len(events) != 0 {
		t.Fatal("forged report was recorded", events)
	}
}

func TestBrowserPoliciesReturnsEmptyArray(t *testing.T) {
	if encoded, err := json.Marshal(browserPolicies(nil, nil)); err != nil || string(encoded) != "[]" {
		t.Fatalf("empty browser policy set must be an array: %s, %v", encoded, err)
	}
}

func browserTestPolicy(id, ruleID model.ID) policy.Policy {
	return policy.Policy{Version: 1, ID: id, Name: string(id), Rules: []policy.Rule{{
		ID: ruleID, Name: string(ruleID), Priority: 100, Enabled: true, StopProcessing: true,
		Conditions: policy.Condition{Field: "resource_type", Operator: "equals", Values: []string{"image"}},
		Actions:    []policy.Action{{Type: "block"}},
	}}}
}
