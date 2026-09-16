package policy

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"net/netip"
	"testing"
	"time"
)

func rule(id string, priority int, condition Condition, action Action, stop bool) Rule {
	return Rule{ID: model.ID(id), Name: id, Priority: priority, Enabled: true, Conditions: condition, Actions: []Action{action}, StopProcessing: stop}
}
func TestEvaluatePriorityUnknownAndTrace(t *testing.T) {
	p := Policy{Version: 1, ID: "policy", Name: "p", Rules: []Rule{rule("low", 1, Condition{Field: "host", Operator: "suffix", Values: []string{"example.invalid"}}, Action{Type: "direct"}, true), rule("high", 10, Condition{Field: "path", Operator: "equals", Values: []string{"/hidden"}}, Action{Type: "block"}, true)}}
	r := RequestContext{Host: "a.example.invalid", Method: model.Optional[string]{Known: true, Value: "GET"}, Timestamp: time.Now()}
	got, err := Evaluate(p, r, Visibility{Host: true, Method: true}, true)
	if err != nil || got.PolicyID != "policy" || got.TerminalRuleID != "low" || len(got.Actions) != 1 || got.Actions[0].Type != "direct" || len(got.MatchedRuleIDs) != 1 || got.MatchedRuleIDs[0] != "low" {
		t.Fatalf("%+v %v", got, err)
	}
	if len(got.Trace.UnknownFields) != 1 || got.Trace.UnknownFields[0] != "path" {
		t.Fatal(got.Trace)
	}
}
func TestEvaluateConditionComposition(t *testing.T) {
	p := Policy{Version: 1, ID: "p", Name: "p", Rules: []Rule{rule("r", 1, Condition{All: []Condition{{Field: "host", Operator: "wildcard", Values: []string{"*.example.invalid"}}, {Not: &Condition{Field: "method", Operator: "equals", Values: []string{"POST"}}}}}, Action{Type: "proxy", PoolID: "pool"}, true)}}
	r := RequestContext{Host: "x.example.invalid", Method: model.Optional[string]{Known: true, Value: "GET"}}
	got, err := Evaluate(p, r, Visibility{Host: true, Method: true}, false)
	if err != nil || got.Actions[0].PoolID != "pool" {
		t.Fatal(got, err)
	}
	r.Method.Value = "POST"
	_, err = Evaluate(p, r, Visibility{Host: true, Method: true}, false)
	if !errors.Is(err, ErrNoRoute) {
		t.Fatal(err)
	}
}
func TestEvaluateCIDRRegexAndNoRoute(t *testing.T) {
	p := Policy{Version: 1, ID: "p", Name: "p", Rules: []Rule{rule("r", 1, Condition{Any: []Condition{{Field: "destination_ip", Operator: "cidr", Values: []string{"203.0.113.0/24"}}, {Field: "host", Operator: "regex", Values: []string{"^ok\\."}}}}, Action{Type: "reject"}, true)}}
	r := RequestContext{Host: "no.invalid", DestinationIP: netip.MustParseAddr("203.0.113.7")}
	if _, err := Evaluate(p, r, Visibility{Host: true}, false); err != nil {
		t.Fatal(err)
	}
	r.DestinationIP = netip.MustParseAddr("198.51.100.7")
	if _, err := Evaluate(p, r, Visibility{Host: true}, false); !errors.Is(err, ErrNoRoute) {
		t.Fatal(err)
	}
}

func TestValidateRejectsUnsupportedOrMalformedConditions(t *testing.T) {
	document := Policy{Version: 1, ID: "p", Name: "Policy", Rules: []Rule{rule("r", 1, Condition{Field: "host", Operator: "unsupported", Values: []string{"example.invalid"}}, Action{Type: "reject"}, true)}}
	if document.Validate() == nil {
		t.Fatal("unsupported operator accepted")
	}
	document.Rules[0].Conditions = Condition{Field: "host", Operator: "regex", Values: []string{"["}}
	if document.Validate() == nil {
		t.Fatal("invalid regex accepted")
	}
	document.Rules[0].Conditions = Condition{Field: "destination_ip", Operator: "cidr", Values: []string{"not-a-prefix"}}
	if document.Validate() == nil {
		t.Fatal("invalid CIDR accepted")
	}
	document.Rules[0].Conditions = Condition{Field: "host", Operator: "suffix", Values: []string{"example.invalid"}}
	if document.Validate() != nil {
		t.Fatal("supported condition rejected")
	}
}

func TestValidateAdvancedActionValues(t *testing.T) {
	valid := []Action{
		{Type: "cache"},
		{Type: "throttle", Value: "1048576"},
		{Type: "mock", Value: "ok"},
		{Type: "redirect", Value: "https://example.invalid/login"},
		{Type: "redirect", Value: "/login"},
		{Type: "rewrite", Value: "/v2/items?limit=10"},
	}
	for _, action := range valid {
		if !action.Valid() {
			t.Fatalf("valid action rejected: %+v", action)
		}
	}
	invalid := []Action{
		{Type: "allow"},
		{Type: "set_tag", Value: "residential"},
		{Type: "set_session_policy", Value: "sticky"},
		{Type: "cache", Value: "pool"},
		{Type: "throttle", Value: "0"},
		{Type: "throttle", Value: "fast"},
		{Type: "redirect", Value: "javascript:alert(1)"},
		{Type: "redirect", Value: "//evil.invalid"},
		{Type: "rewrite", Value: "https://evil.invalid/"},
		{Type: "rewrite", Value: "relative"},
	}
	for _, action := range invalid {
		if action.Valid() {
			t.Fatalf("invalid action accepted: %+v", action)
		}
	}
}

func TestAdvancedModifiersRequireTerminalRoute(t *testing.T) {
	document := Policy{Version: 1, ID: "p", Name: "p", Rules: []Rule{{ID: "r", Name: "r", Enabled: true, Actions: []Action{{Type: "cache"}, {Type: "rewrite", Value: "/small"}, {Type: "throttle", Value: "1024"}, {Type: "direct"}}}}}
	result, err := Evaluate(document, RequestContext{}, Visibility{}, false)
	if err != nil || result.TerminalRuleID != "r" || len(result.Actions) != 4 {
		t.Fatal(result, err)
	}
	document.Rules[0].Actions = document.Rules[0].Actions[:3]
	if _, err = Evaluate(document, RequestContext{}, Visibility{}, false); !errors.Is(err, ErrNoRoute) {
		t.Fatal(err)
	}
}
