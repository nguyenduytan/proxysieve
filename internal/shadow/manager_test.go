package shadow

import (
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestManagerRecordsDifferencesWithoutChangingActiveResult(t *testing.T) {
	config := publicshadow.Config{
		ID: "candidate", Name: "Candidate", ActivePolicyID: "active", Enabled: true,
		Policy: policy.Policy{Version: 1, ID: "candidate-policy", Name: "Candidate", Rules: []policy.Rule{{
			ID: "block", Name: "Block", Enabled: true, StopProcessing: true,
			Conditions: policy.Condition{}, Actions: []policy.Action{{Type: "block"}},
		}}},
	}
	manager, err := New([]store.ShadowRecord{{Shadow: config, Revision: 1}})
	if err != nil {
		t.Fatal(err)
	}
	request := policy.RequestContext{Host: "example.invalid", ContentLength: model.Optional[int64]{Known: true, Value: 42}}
	active := policy.Result{PolicyID: "active", Actions: []policy.Action{{Type: "proxy", PoolID: "pool-a"}}}
	manager.Observe("active", request, policy.Visibility{Host: true}, active, nil)

	comparison, ok := manager.Comparison(config.ID)
	if !ok || comparison.Samples != 1 || comparison.DifferentDecisions != 1 || comparison.ActiveDecisions["proxy"] != 1 || comparison.ShadowDecisions["block"] != 1 || active.Actions[0].PoolID != "pool-a" {
		t.Fatalf("comparison=%+v active=%+v", comparison, active)
	}

	config.Policy.Rules[0].Actions[0] = policy.Action{Type: "proxy", PoolID: "pool-b"}
	oldGeneration := manager.states[config.ID].generation
	manager.Upsert(config)
	comparison, _ = manager.Comparison(config.ID)
	if comparison.Samples != 0 {
		t.Fatalf("comparison was not reset after policy change: %+v", comparison)
	}
	manager.record(config.ID, oldGeneration, active, nil, policy.Result{Actions: []policy.Action{{Type: "block"}}}, nil, 1, request)
	comparison, _ = manager.Comparison(config.ID)
	if comparison.Samples != 0 {
		t.Fatalf("stale evaluation contaminated replacement: %+v", comparison)
	}
	manager.Observe("active", request, policy.Visibility{Host: true}, active, nil)
	comparison, _ = manager.Comparison(config.ID)
	if comparison.PoolDifferences != 1 || comparison.EstimatedUpstreamBytes != 42 || comparison.AverageEvaluationNS < 0 {
		t.Fatalf("comparison=%+v", comparison)
	}
}
