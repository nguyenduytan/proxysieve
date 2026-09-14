package memory

import (
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestPolicyContract(t *testing.T) {
	contract.RunPolicies(t, func(t *testing.T) store.Policies {
		t.Helper()
		repository, err := NewPolicies(100)
		if err != nil {
			t.Fatal(err)
		}
		return repository
	})
}
