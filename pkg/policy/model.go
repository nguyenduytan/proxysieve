// Package policy defines versioned policy documents; evaluation is implemented in M5.
package policy

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/netip"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid policy")

type Condition struct {
	Field    string      `json:"field,omitempty"`
	Operator string      `json:"operator,omitempty"`
	Values   []string    `json:"values,omitempty"`
	All      []Condition `json:"all,omitempty"`
	Any      []Condition `json:"any,omitempty"`
	Not      *Condition  `json:"not,omitempty"`
}
type Action struct {
	Type   string   `json:"type"`
	PoolID model.ID `json:"pool_id,omitempty"`
	Value  string   `json:"value,omitempty"`
}
type Rule struct {
	ID             model.ID  `json:"id"`
	Name           string    `json:"name"`
	Priority       int       `json:"priority"`
	Enabled        bool      `json:"enabled"`
	StopProcessing bool      `json:"stop_processing"`
	Conditions     Condition `json:"conditions"`
	Actions        []Action  `json:"actions"`
}
type Policy struct {
	Version int      `json:"version"`
	ID      model.ID `json:"id"`
	Name    string   `json:"name"`
	Rules   []Rule   `json:"rules"`
}

func (p Policy) Validate() error {
	if p.Version != 1 || !p.ID.Valid() || strings.TrimSpace(p.Name) == "" || len(p.Name) > 256 || len(p.Rules) > 10_000 {
		return ErrInvalid
	}
	seen := map[model.ID]bool{}
	for _, r := range p.Rules {
		if !r.ID.Valid() || seen[r.ID] || len(r.Actions) == 0 || len(r.Actions) > 16 {
			return ErrInvalid
		}
		seen[r.ID] = true
		if !validCondition(r.Conditions, 0) {
			return ErrInvalid
		}
		for _, a := range r.Actions {
			if !a.Valid() {
				return ErrInvalid
			}
		}
	}
	return nil
}
func validCondition(c Condition, depth int) bool {
	if depth > 16 {
		return false
	}
	forms := 0
	if c.Field != "" || c.Operator != "" || len(c.Values) > 0 {
		forms++
	}
	if len(c.All) > 0 {
		forms++
	}
	if len(c.Any) > 0 {
		forms++
	}
	if c.Not != nil {
		forms++
	}
	if forms == 0 {
		return true
	}
	if forms != 1 {
		return false
	}
	if len(c.All) > 0 {
		for _, child := range c.All {
			if !validCondition(child, depth+1) {
				return false
			}
		}
		return true
	}
	if len(c.Any) > 0 {
		for _, child := range c.Any {
			if !validCondition(child, depth+1) {
				return false
			}
		}
		return true
	}
	if c.Not != nil {
		return validCondition(*c.Not, depth+1)
	}
	return model.ID(c.Field).Valid() && model.ID(c.Operator).Valid() && len(c.Values) > 0 && len(c.Values) <= 4096
}
func (a Action) Valid() bool {
	switch a.Type {
	case "allow", "block", "reject", "direct", "cache", "throttle", "mock", "redirect", "rewrite", "set_tag", "set_session_policy":
		return a.PoolID == "" && len(a.Value) <= 4096
	case "proxy":
		return a.PoolID.Valid() && a.Value == ""
	}
	return false
}

// RequestContext records only observed values. Known false differs from empty data.
// It is not a logging DTO: visible headers can contain secrets and need redaction.
type RequestContext struct {
	RequestID, ConnectionID, ClientID    model.ID
	Listener, Protocol, Scheme, Host     string
	Port                                 uint16
	SourceIP, DestinationIP              netip.Addr
	Method, Path, ResourceType, MIMEHint model.Optional[string]
	ContentLength                        model.Optional[int64]
	Headers                              model.Optional[map[string][]string]
	Timestamp                            time.Time
	InspectMode                          bool
}
