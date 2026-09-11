// Package downstreamauth verifies API keys without exposing their raw values.
package downstreamauth

import (
	"context"
	"errors"
	"strings"

	"github.com/nguyenduytan/proxysieve/internal/keys"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

var ErrUnauthorized = errors.New("downstream authentication failed")

type Store interface {
	FindAPIKey(context.Context, [32]byte) (auth.APIKey, error)
	FindClientAuth(context.Context, model.ID) (auth.Client, error)
}

// AuthenticateBearer accepts the standard Proxy-Authorization Bearer scheme.
// It returns only a client ID; no downstream code receives the raw key after this.
func AuthenticateBearer(ctx context.Context, header string, store Store) (model.ID, error) {
	if store == nil {
		return "", ErrUnauthorized
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || !keys.Valid(token) {
		return "", ErrUnauthorized
	}
	key, err := store.FindAPIKey(ctx, keys.Hash(token))
	if err != nil || key.RevokedAt != nil {
		return "", ErrUnauthorized
	}
	client, err := store.FindClientAuth(ctx, key.ClientID)
	if err != nil || !client.Enabled || client.AuthMethod != "api_key" {
		return "", ErrUnauthorized
	}
	return key.ClientID, nil
}
