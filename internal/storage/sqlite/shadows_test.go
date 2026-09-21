package sqlite

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	publicshadow "github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestShadowInventory(t *testing.T) {
	repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "shadow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()

	config := publicshadow.Config{ID: "candidate", Name: "Candidate", ActivePolicyID: "active", Policy: contract.Policy("candidate-policy"), Enabled: true}
	record, err := repository.PutShadow(t.Context(), config, 0)
	if err != nil || record.Revision != 1 {
		t.Fatal(record, err)
	}
	config.Name = "Updated"
	record, err = repository.PutShadow(t.Context(), config, record.Revision)
	if err != nil || record.Revision != 2 {
		t.Fatal(record, err)
	}
	config.Policy.Name = "caller mutation"
	stored, err := repository.GetShadow(t.Context(), config.ID)
	if err != nil || stored.Shadow.Policy.Name == config.Policy.Name {
		t.Fatal(stored, err)
	}
	items, err := repository.ListShadows(t.Context())
	if err != nil || len(items) != 1 || items[0].Shadow.ID != config.ID {
		t.Fatal(items, err)
	}
	if err = repository.DeleteShadow(t.Context(), config.ID, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if err = repository.DeleteShadow(t.Context(), config.ID, 2); err != nil {
		t.Fatal(err)
	}
}
