package policy

import (
	"errors"
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var ErrNoRoute = errors.New("policy did not produce a route")
var ErrInvalidDocument = errors.New("invalid policy document")

type MatchState uint8

const (
	Unknown MatchState = iota
	NoMatch
	Match
)

type Visibility struct{ Host, Method, Path, Headers, ResourceType bool }
type ConditionTrace struct {
	Field, Operator string
	State           MatchState
	Reason          string
}
type RuleTrace struct {
	RuleID     model.ID
	Matched    bool
	Conditions []ConditionTrace
}
type Trace struct {
	Rules         []RuleTrace
	UnknownFields []string
}
type Result struct {
	Actions         []Action
	MatchedRuleIDs  []model.ID
	Trace           Trace
	RuntimeRevision int64
}

// Evaluate has deterministic ordering: priority desc, then document order. A rule
// matches only when all required conditions are known matches. Unknown is recorded
// and cannot accidentally grant direct/proxy access. Evaluation itself is pure.
func Evaluate(document Policy, request RequestContext, visibility Visibility, tracing bool) (Result, error) {
	if document.Validate() != nil {
		return Result{}, ErrInvalidDocument
	}
	rules := slices.Clone(document.Rules)
	slices.SortStableFunc(rules, func(a, b Rule) int {
		if a.Priority > b.Priority {
			return -1
		}
		if a.Priority < b.Priority {
			return 1
		}
		return 0
	})
	var result Result
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		state, conditions := matchCondition(rule.Conditions, request, visibility)
		if tracing {
			result.Trace.Rules = append(result.Trace.Rules, RuleTrace{RuleID: rule.ID, Matched: state == Match, Conditions: conditions})
			for _, c := range conditions {
				if c.State == Unknown {
					result.Trace.UnknownFields = append(result.Trace.UnknownFields, c.Field)
				}
			}
		}
		if state != Match {
			continue
		}
		result.MatchedRuleIDs = append(result.MatchedRuleIDs, rule.ID)
		result.Actions = append(result.Actions, rule.Actions...)
		if rule.StopProcessing {
			break
		}
	}
	if !hasTerminal(result.Actions) {
		return result, ErrNoRoute
	}
	return result, nil
}
func hasTerminal(actions []Action) bool {
	for _, a := range actions {
		switch a.Type {
		case "block", "reject", "proxy", "direct", "cache", "mock", "redirect", "rewrite":
			return true
		}
	}
	return false
}
func matchCondition(c Condition, r RequestContext, v Visibility) (MatchState, []ConditionTrace) {
	if len(c.All) > 0 {
		return matchAll(c.All, r, v)
	}
	if len(c.Any) > 0 {
		return matchAny(c.Any, r, v)
	}
	if c.Not != nil {
		s, t := matchCondition(*c.Not, r, v)
		switch s {
		case Match:
			s = NoMatch
		case NoMatch:
			s = Match
		}
		return s, t
	}
	if c.Field == "" {
		return Match, nil
	}
	return matchLeaf(c, r, v)
}
func matchAll(children []Condition, r RequestContext, v Visibility) (MatchState, []ConditionTrace) {
	state := Match
	var traces []ConditionTrace
	for _, child := range children {
		s, t := matchCondition(child, r, v)
		traces = append(traces, t...)
		if s == NoMatch {
			return NoMatch, traces
		}
		if s == Unknown {
			state = Unknown
		}
	}
	return state, traces
}
func matchAny(children []Condition, r RequestContext, v Visibility) (MatchState, []ConditionTrace) {
	state := NoMatch
	var traces []ConditionTrace
	for _, child := range children {
		s, t := matchCondition(child, r, v)
		traces = append(traces, t...)
		if s == Match {
			return Match, traces
		}
		if s == Unknown {
			state = Unknown
		}
	}
	return state, traces
}
func matchLeaf(c Condition, r RequestContext, v Visibility) (MatchState, []ConditionTrace) {
	trace := ConditionTrace{Field: c.Field, Operator: c.Operator}
	known := true
	value := ""
	switch c.Field {
	case "host":
		known = v.Host
		value = r.Host
	case "listener":
		value = r.Listener
	case "client":
		value = string(r.ClientID)
	case "protocol":
		value = r.Protocol
	case "scheme":
		value = r.Scheme
	case "method":
		known = v.Method && r.Method.Known
		value = r.Method.Value
	case "path":
		known = v.Path && r.Path.Known
		value = r.Path.Value
	case "resource_type":
		known = v.ResourceType && r.ResourceType.Known
		value = r.ResourceType.Value
	case "destination_ip":
		if !r.DestinationIP.IsValid() {
			known = false
		} else {
			value = r.DestinationIP.String()
		}
	case "hour_utc":
		value = time.Unix(0, 0).UTC().Format("15")
		if !r.Timestamp.IsZero() {
			value = r.Timestamp.UTC().Format("15")
		}
	default:
		known = false
	}
	if !known {
		trace.State = Unknown
		trace.Reason = "attribute unavailable"
		return Unknown, []ConditionTrace{trace}
	}
	matched := false
	switch c.Operator {
	case "equals":
		for _, want := range c.Values {
			if value == want {
				matched = true
			}
		}
	case "any":
		for _, want := range c.Values {
			if value == want {
				matched = true
			}
		}
	case "suffix":
		for _, want := range c.Values {
			if strings.HasSuffix(strings.ToLower(value), strings.ToLower(want)) {
				matched = true
			}
		}
	case "wildcard":
		for _, want := range c.Values {
			if ok, _ := path.Match(strings.ToLower(want), strings.ToLower(value)); ok {
				matched = true
			}
		}
	case "regex":
		for _, want := range c.Values {
			re, err := regexp.Compile(want)
			if err == nil && re.MatchString(value) {
				matched = true
			}
		}
	case "cidr":
		addr, err := netip.ParseAddr(value)
		if err == nil {
			for _, want := range c.Values {
				prefix, e := netip.ParsePrefix(want)
				if e == nil && prefix.Contains(addr) {
					matched = true
				}
			}
		}
	}
	if matched {
		trace.State = Match
	} else {
		trace.State = NoMatch
	}
	return trace.State, []ConditionTrace{trace}
}
