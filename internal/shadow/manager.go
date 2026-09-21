package shadow

import (
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const maxConfigs = 64

type state struct {
	config       publicshadow.Config
	comparison   publicshadow.Comparison
	totalLatency uint64
	generation   uint64
}

type Manager struct {
	mu     sync.RWMutex
	states map[model.ID]*state
	now    func() time.Time
	next   uint64
}

func New(records []store.ShadowRecord) (*Manager, error) {
	if len(records) > maxConfigs {
		return nil, store.ErrInvalid
	}
	m := &Manager{states: make(map[model.ID]*state, len(records)), now: func() time.Time { return time.Now().UTC() }}
	for _, record := range records {
		if record.Shadow.Validate() != nil || record.Revision < 1 {
			return nil, store.ErrSchema
		}
		m.states[record.Shadow.ID] = m.newState(record.Shadow)
	}
	return m, nil
}

func (m *Manager) newState(config publicshadow.Config) *state {
	m.next++
	config = config.Clone()
	return &state{config: config, comparison: publicshadow.Comparison{
		ShadowID: config.ID, ActivePolicyID: config.ActivePolicyID, ShadowPolicyID: config.Policy.ID,
		ActiveDecisions: map[string]uint64{}, ShadowDecisions: map[string]uint64{},
	}, generation: m.next}
}

func (m *Manager) Upsert(config publicshadow.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.states[config.ID]; ok && current.config.ActivePolicyID == config.ActivePolicyID && reflect.DeepEqual(current.config.Policy, config.Policy) {
		current.config = config.Clone()
		return
	}
	m.states[config.ID] = m.newState(config)
}

func (m *Manager) Delete(id model.ID) {
	m.mu.Lock()
	delete(m.states, id)
	m.mu.Unlock()
}

func (m *Manager) Comparison(id model.ID) (publicshadow.Comparison, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	current, ok := m.states[id]
	if !ok {
		return publicshadow.Comparison{}, false
	}
	comparison := current.comparison
	comparison.ActiveDecisions = cloneCounts(comparison.ActiveDecisions)
	comparison.ShadowDecisions = cloneCounts(comparison.ShadowDecisions)
	return comparison, true
}

func (m *Manager) Observe(activePolicyID model.ID, request policy.RequestContext, visibility policy.Visibility, active policy.Result, activeErr error) {
	m.mu.RLock()
	type candidate struct {
		config     publicshadow.Config
		generation uint64
	}
	eligible := make([]candidate, 0, len(m.states))
	for _, current := range m.states {
		if current.config.Enabled && current.config.ActivePolicyID == activePolicyID {
			eligible = append(eligible, candidate{config: current.config.Clone(), generation: current.generation})
		}
	}
	m.mu.RUnlock()
	for _, candidate := range eligible {
		started := time.Now()
		result, err := policy.Evaluate(candidate.config.Policy, request, visibility, false)
		m.record(candidate.config.ID, candidate.generation, active, activeErr, result, err, time.Since(started), request)
	}
}

func (m *Manager) record(id model.ID, generation uint64, active policy.Result, activeErr error, simulated policy.Result, simulatedErr error, latency time.Duration, request policy.RequestContext) {
	activeDecision, activeTarget := decision(active, activeErr)
	shadowDecision, shadowTarget := decision(simulated, simulatedErr)
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.states[id]
	if !ok || current.generation != generation {
		return
	}
	c := &current.comparison
	c.Samples++
	c.ActiveDecisions[activeDecision]++
	c.ShadowDecisions[shadowDecision]++
	if activeDecision == shadowDecision && activeTarget == shadowTarget {
		c.SameDecisions++
	} else {
		c.DifferentDecisions++
	}
	if activeDecision == "proxy" && shadowDecision == "proxy" && activeTarget != shadowTarget {
		c.PoolDifferences++
	}
	if simulatedErr != nil && !errors.Is(simulatedErr, policy.ErrNoRoute) {
		c.EvaluationErrors++
	}
	if request.ContentLength.Known && request.ContentLength.Value > 0 && (shadowDecision == "proxy" || shadowDecision == "chain" || shadowDecision == "direct") {
		c.EstimatedUpstreamBytes += uint64(request.ContentLength.Value)
	}
	current.totalLatency += uint64(latency)
	c.AverageEvaluationNS = int64(current.totalLatency / c.Samples)
	c.UpdatedAt = m.now()
}

func decision(result policy.Result, err error) (string, model.ID) {
	if errors.Is(err, policy.ErrNoRoute) {
		return "no_route", ""
	}
	if err != nil {
		return "error", ""
	}
	for _, action := range result.Actions {
		switch action.Type {
		case "proxy":
			return action.Type, action.PoolID
		case "chain":
			return action.Type, action.ChainID
		case "block", "reject", "direct", "mock", "redirect":
			return action.Type, ""
		}
	}
	return "no_route", ""
}

func cloneCounts(source map[string]uint64) map[string]uint64 {
	clone := make(map[string]uint64, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
