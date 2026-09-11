// Package secrets contains non-persistent secret resolvers for local development.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/nguyenduytan/proxysieve/internal/upstream"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

var ErrUnavailable = errors.New("secret is unavailable")

type Environment struct{ Lookup func(string) (string, bool) }

func (e Environment) ResolveCredentials(_ context.Context, ref secret.Ref) (upstream.Credentials, error) {
	if !ref.Valid() {
		return upstream.Credentials{}, ErrUnavailable
	}
	lookup := e.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	raw, ok := lookup(variable(ref))
	if !ok || len(raw) == 0 || len(raw) > secret.MaxBytes {
		return upstream.Credentials{}, ErrUnavailable
	}
	var value struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return upstream.Credentials{}, ErrUnavailable
	}
	username, err := secret.New([]byte(value.Username))
	if err != nil {
		return upstream.Credentials{}, ErrUnavailable
	}
	password, err := secret.New([]byte(value.Password))
	if err != nil {
		return upstream.Credentials{}, ErrUnavailable
	}
	return upstream.Credentials{Username: username, Password: password}, nil
}
func variable(ref secret.Ref) string {
	parts := strings.Split(strings.TrimPrefix(string(ref), "secret://"), "/")
	for i := range parts {
		parts[i] = strings.ToUpper(strings.ReplaceAll(parts[i], "-", "_"))
	}
	return "PROXYSIEVE_SECRET_" + strings.Join(parts, "_")
}

var _ upstream.CredentialResolver = Environment{}
