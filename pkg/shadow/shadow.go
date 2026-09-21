// Package shadow defines simulation-only policy configurations and comparisons.
package shadow

import (
	"errors"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

var ErrInvalid = errors.New("invalid shadow policy")

type Config struct {
	ID             model.ID      `json:"id"`
	Name           string        `json:"name"`
	ActivePolicyID model.ID      `json:"active_policy_id"`
	Policy         policy.Policy `json:"policy"`
	Enabled        bool          `json:"enabled"`
}

func (c Config) Validate() error {
	if !c.ID.Valid() || strings.TrimSpace(c.Name) == "" || len(c.Name) > 256 || !c.ActivePolicyID.Valid() || c.Policy.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

func (c Config) Clone() Config {
	c.Policy = c.Policy.Clone()
	return c
}

type Comparison struct {
	ShadowID               model.ID          `json:"shadow_id"`
	ActivePolicyID         model.ID          `json:"active_policy_id"`
	ShadowPolicyID         model.ID          `json:"shadow_policy_id"`
	Samples                uint64            `json:"samples"`
	SameDecisions          uint64            `json:"same_decisions"`
	DifferentDecisions     uint64            `json:"different_decisions"`
	PoolDifferences        uint64            `json:"pool_differences"`
	EvaluationErrors       uint64            `json:"evaluation_errors"`
	EstimatedUpstreamBytes uint64            `json:"estimated_upstream_bytes"`
	AverageEvaluationNS    int64             `json:"average_evaluation_ns"`
	ActiveDecisions        map[string]uint64 `json:"active_decisions"`
	ShadowDecisions        map[string]uint64 `json:"shadow_decisions"`
	UpdatedAt              time.Time         `json:"updated_at,omitempty"`
}
