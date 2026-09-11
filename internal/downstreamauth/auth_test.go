package downstreamauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/keys"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

type store struct {
	key      auth.APIKey
	client   auth.Client
	expected [32]byte
	err      error
}

func (s store) FindAPIKey(_ context.Context, hash [32]byte) (auth.APIKey, error) {
	if s.err != nil || hash != s.expected {
		return auth.APIKey{}, errors.New("missing")
	}
	return s.key, nil
}
func (s store) FindClientAuth(_ context.Context, id model.ID) (auth.Client, error) {
	if id != s.client.ID {
		return auth.Client{}, errors.New("missing")
	}
	return s.client, nil
}
func TestBearerAuthentication(t *testing.T) {
	token, _, hash, _ := keys.Generate()
	client := auth.Client{ID: "client", Name: "Client", Enabled: true, AuthMethod: "api_key", CreatedAt: time.Now().UTC()}
	s := store{key: auth.APIKey{ID: "key", ClientID: "client", Prefix: token[:12], CreatedAt: time.Now().UTC()}, client: client, expected: hash}
	id, err := AuthenticateBearer(context.Background(), "Bearer "+token, s)
	if err != nil || id != "client" {
		t.Fatal(id, err)
	}
	for _, header := range []string{"", token, "Basic " + token, "Bearer bad"} {
		if _, err := AuthenticateBearer(context.Background(), header, s); !errors.Is(err, ErrUnauthorized) {
			t.Fatal(header, err)
		}
	}
}
