package secrets

import (
	"context"
	"testing"
)

func TestEnvironmentCredentials(t *testing.T) {
	e := Environment{Lookup: func(k string) (string, bool) {
		if k == "PROXYSIEVE_SECRET_UPSTREAM_AUTH" {
			return `{"username":"demo","password":"fake-password"}`, true
		}
		return "", false
	}}
	value, err := e.ResolveCredentials(context.Background(), "secret://upstream/auth")
	if err != nil || string(value.Username.Reveal()) != "demo" || string(value.Password.Reveal()) != "fake-password" {
		t.Fatal(value, err)
	}
	if _, err = e.ResolveCredentials(context.Background(), "secret://upstream/missing"); err == nil {
		t.Fatal("missing secret accepted")
	}
	if variable("secret://up-stream/auth-key") != "PROXYSIEVE_SECRET_UP_STREAM_AUTH_KEY" {
		t.Fatal(variable("secret://up-stream/auth-key"))
	}
}
