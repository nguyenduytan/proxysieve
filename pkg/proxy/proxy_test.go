package proxy

import (
	"testing"
	"time"
)

func TestHostValidation(t *testing.T) {
	for _, host := range []string{"example.invalid", "EXAMPLE.invalid.", "127.0.0.1", "::1", "2001:db8::1"} {
		if !ValidHost(host) {
			t.Fatal("valid host rejected", host)
		}
	}
	for _, host := range []string{"", "-bad.invalid", "bad-.invalid", "a..b", "[::1]", "user:pass@example.invalid", "https://example.invalid", "example.invalid/path", "fe80::1%eth0", "a\nb"} {
		if ValidHost(host) {
			t.Fatal("invalid host accepted", host)
		}
	}
}
func TestEndpointValidation(t *testing.T) {
	e := Endpoint{ID: "test", Name: "proxy", Protocol: HTTP, Host: "example.invalid", Port: 8080}
	if e.Validate() != nil {
		t.Fatal("valid rejected")
	}
	if e.String() != "http://example.invalid:8080" {
		t.Fatal(e.String())
	}
	e.CredentialRef = "secret://proxy/test"
	if e.Validate() != nil {
		t.Fatal("ref rejected")
	}
	e.Host = "user:fake-secret@example.invalid"
	if e.Validate() == nil || e.String() != "[invalid endpoint]" {
		t.Fatal("unsafe endpoint")
	}
}

func TestSourceValidationAndClone(t *testing.T) {
	source := Source{ID: "source", Name: "Source", Type: APISource, Config: map[string]string{"url": "https://source.example.invalid/list"}, RefreshInterval: time.Hour, Enabled: true}
	if source.Validate() != nil {
		t.Fatal("valid source rejected")
	}
	clone := source.Clone()
	clone.Config["url"] = "mutated"
	if source.Config["url"] != "https://source.example.invalid/list" {
		t.Fatal("source clone aliases config")
	}
	source.Name = ""
	if source.Validate() == nil {
		t.Fatal("nameless source accepted")
	}
	source.Name = "Source"
	source.Config["api_token"] = "must-not-be-stored"
	if source.Validate() == nil {
		t.Fatal("sensitive source config key accepted")
	}
}
