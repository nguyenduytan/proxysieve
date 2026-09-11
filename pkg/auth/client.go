// Package auth defines client identities separately from authentication mechanisms.
package auth

import (
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/netip"
	"time"
)

type Client struct {
	ID               model.ID       `json:"id"`
	Name             string         `json:"name"`
	Enabled          bool           `json:"enabled"`
	AuthMethod       string         `json:"auth_method"`
	AllowedListeners []string       `json:"allowed_listeners"`
	AllowedPools     []model.ID     `json:"allowed_pools"`
	PolicyIDs        []model.ID     `json:"policy_ids"`
	BudgetIDs        []model.ID     `json:"budget_ids"`
	IPAllowlist      []netip.Prefix `json:"ip_allowlist"`
	CreatedAt        time.Time      `json:"created_at"`
	LastSeenAt       time.Time      `json:"last_seen_at"`
}

// APIKey is metadata only. Credential hashes belong to a dedicated auth store and
// raw API keys must be returned only by the creation operation in M11.
type APIKey struct {
	ID        model.ID   `json:"id"`
	ClientID  model.ID   `json:"client_id"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
