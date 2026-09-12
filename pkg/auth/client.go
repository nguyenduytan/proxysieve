// Package auth defines client identities separately from authentication mechanisms.
package auth

import (
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/netip"
	"slices"
	"strings"
	"time"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool { return r == RoleAdmin || r == RoleOperator || r == RoleViewer }

type User struct {
	ID          model.ID  `json:"id"`
	Username    string    `json:"username"`
	Role        Role      `json:"role"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

func (u User) Validate() bool {
	return u.ID.Valid() && len(u.Username) >= 3 && len(u.Username) <= 64 && strings.TrimSpace(u.Username) == u.Username && u.Role.Valid() && !u.CreatedAt.IsZero()
}

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

func (c Client) Validate() bool {
	if !c.ID.Valid() || len(c.Name) < 1 || len(c.Name) > 256 || strings.TrimSpace(c.Name) != c.Name || c.AuthMethod != "api_key" || c.CreatedAt.IsZero() {
		return false
	}
	if len(c.AllowedListeners) > 256 || len(c.AllowedPools) > 256 || len(c.PolicyIDs) > 256 || len(c.BudgetIDs) > 256 || len(c.IPAllowlist) > 256 {
		return false
	}
	for _, listener := range c.AllowedListeners {
		if len(listener) < 1 || len(listener) > 128 || strings.TrimSpace(listener) != listener {
			return false
		}
	}
	for _, ids := range [][]model.ID{c.AllowedPools, c.PolicyIDs, c.BudgetIDs} {
		for _, id := range ids {
			if !id.Valid() {
				return false
			}
		}
	}
	for _, prefix := range c.IPAllowlist {
		if !prefix.IsValid() {
			return false
		}
	}
	return true
}

func (c Client) Clone() Client {
	c.AllowedListeners = slices.Clone(c.AllowedListeners)
	c.AllowedPools = slices.Clone(c.AllowedPools)
	c.PolicyIDs = slices.Clone(c.PolicyIDs)
	c.BudgetIDs = slices.Clone(c.BudgetIDs)
	c.IPAllowlist = slices.Clone(c.IPAllowlist)
	return c
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
