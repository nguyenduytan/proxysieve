package sqlite

import (
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
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
	record, err := s.PutClient(t.Context(), client, 0)
	if err != nil || record.Revision != 1 {
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
	if err = s.RevokeAPIKey(t.Context(), client.ID, model.ID("key"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	found, err = s.FindAPIKey(t.Context(), hash)
	if err != nil || found.RevokedAt == nil {
		t.Fatal(found, err)
	}
	client.Name = "Updated client"
	record, err = s.PutClient(t.Context(), client, record.Revision)
	if err != nil || record.Revision != 2 || record.Client.Name != client.Name {
		t.Fatal(record, err)
	}
	if _, err = s.PutClient(t.Context(), client, 1); err != store.ErrConflict {
		t.Fatal("stale client update", err)
	}
	if err = s.DeleteClient(t.Context(), client.ID, record.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FindAPIKey(t.Context(), hash); err != store.ErrNotFound {
		t.Fatal("client deletion did not cascade API keys", err)
	}
}
