package sqlite

import (
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"testing"
	"time"
)

func TestClientAndAPIKeyPersistence(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now().UTC()
	client := auth.Client{ID: "client", Name: "Client", Enabled: true, AuthMethod: "api_key", CreatedAt: now}
	if err = s.CreateClient(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	hash := [32]byte{1}
	key := auth.APIKey{ID: "key", ClientID: client.ID, Prefix: "psk_1234abcd", CreatedAt: now}
	if _, err = s.CreateAPIKey(t.Context(), key, hash); err != nil {
		t.Fatal(err)
	}
	found, err := s.FindAPIKey(t.Context(), hash)
	if err != nil || found.ID != key.ID || found.RevokedAt != nil {
		t.Fatal(found, err)
	}
	if err = s.RevokeAPIKey(t.Context(), model.ID("key"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	found, err = s.FindAPIKey(t.Context(), hash)
	if err != nil || found.RevokedAt == nil {
		t.Fatal(found, err)
	}
}
