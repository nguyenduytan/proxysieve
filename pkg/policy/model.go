// Package policy defines versioned policy documents; evaluation is implemented in M5.
package policy

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid policy")

type Condition struct {
	Field    string      `json:"field,omitempty" yaml:"field,omitempty"`
	Operator string      `json:"operator,omitempty" yaml:"operator,omitempty"`
	Values   []string    `json:"values,omitempty" yaml:"values,omitempty"`
	All      []Condition `json:"all,omitempty" yaml:"all,omitempty"`
	Any      []Condition `json:"any,omitempty" yaml:"any,omitempty"`
	Not      *Condition  `json:"not,omitempty" yaml:"not,omitempty"`
}
type Action struct {
	Type    string   `json:"type" yaml:"type"`
	PoolID  model.ID `json:"pool_id,omitempty" yaml:"pool_id,omitempty"`
	ChainID model.ID `json:"chain_id,omitempty" yaml:"chain_id,omitempty"`
	Value   string   `json:"value,omitempty" yaml:"value,omitempty"`
}
type Rule struct {
	ID             model.ID  `json:"id" yaml:"id"`
	Name           string    `json:"name" yaml:"name"`
	Priority       int       `json:"priority" yaml:"priority"`
	Enabled        bool      `json:"enabled" yaml:"enabled"`
	StopProcessing bool      `json:"stop_processing" yaml:"stop_processing"`
	Conditions     Condition `json:"conditions" yaml:"conditions"`
	Actions        []Action  `json:"actions" yaml:"actions"`
}
type Policy struct {
	Version int      `json:"version" yaml:"version"`
	ID      model.ID `json:"id" yaml:"id"`
	Name    string   `json:"name" yaml:"name"`
	Rules   []Rule   `json:"rules" yaml:"rules"`
}

func (p Policy) Clone() Policy {
	p.Rules = slices.Clone(p.Rules)
	for i := range p.Rules {
		p.Rules[i].Actions = slices.Clone(p.Rules[i].Actions)
		p.Rules[i].Conditions = p.Rules[i].Conditions.Clone()
	}
	return p
}
func (c Condition) Clone() Condition {
	c.Values = slices.Clone(c.Values)
	c.All = slices.Clone(c.All)
	for i := range c.All {
		c.All[i] = c.All[i].Clone()
	}
	c.Any = slices.Clone(c.Any)
	for i := range c.Any {
		c.Any[i] = c.Any[i].Clone()
	}
	if c.Not != nil {
		clone := c.Not.Clone()
		c.Not = &clone
	}
	return c
}

func (p Policy) Validate() error {
	if p.Version != 1 || !p.ID.Valid() || strings.TrimSpace(p.Name) == "" || len(p.Name) > 256 || len(p.Rules) > 10_000 {
		return ErrInvalid
	}
	seen := map[model.ID]bool{}
	for _, r := range p.Rules {
		if !r.ID.Valid() || seen[r.ID] || strings.TrimSpace(r.Name) == "" || len(r.Name) > 256 || len(r.Actions) == 0 || len(r.Actions) > 16 {
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
	if len(c.All) > 64 || len(c.Any) > 64 {
		return false
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
	if !supportedConditionField(c.Field) || !supportedConditionOperator(c.Operator) || len(c.Values) == 0 || len(c.Values) > 4096 {
		return false
	}
	for _, value := range c.Values {
		if value == "" || len(value) > 4096 {
			return false
		}
		switch c.Operator {
		case "regex":
			if _, err := regexp.Compile(value); err != nil {
				return false
			}
		case "wildcard":
			if _, err := path.Match(value, "validation-target"); err != nil {
				return false
			}
		case "cidr":
			if c.Field != "destination_ip" {
				return false
			}
			if _, err := netip.ParsePrefix(value); err != nil {
				return false
			}
		}
	}
	return true
}

func supportedConditionField(field string) bool {
	switch field {
	case "host", "listener", "client", "protocol", "scheme", "method", "path", "resource_type", "destination_ip", "hour_utc":
		return true
	}
	return false
}

func supportedConditionOperator(operator string) bool {
	switch operator {
	case "equals", "any", "suffix", "wildcard", "regex", "cidr":
		return true
	}
	return false
}
func (a Action) Valid() bool {
	switch a.Type {
	case "allow", "block", "reject", "direct", "cache", "throttle", "mock", "redirect", "rewrite", "set_tag", "set_session_policy":
		return a.PoolID == "" && a.ChainID == "" && len(a.Value) <= 4096
	case "proxy":
		return a.PoolID.Valid() && a.ChainID == "" && a.Value == ""
	case "chain":
		return a.ChainID.Valid() && a.PoolID == "" && a.Value == ""
	}
	return false
}

// RequestContext records only observed values. Known false differs from empty data.
// It is not a logging DTO: visible headers can contain secrets and need redaction.
type RequestContext struct {
	RequestID, ConnectionID, ClientID    model.ID
	Listener, Protocol, Scheme, Host     string
	SessionKey                           string `json:"-"`
	Port                                 uint16
	SourceIP, DestinationIP              netip.Addr
	Method, Path, ResourceType, MIMEHint model.Optional[string]
	ContentLength                        model.Optional[int64]
	Headers                              model.Optional[map[string][]string]
	Timestamp                            time.Time
	InspectMode                          bool
}
