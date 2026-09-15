package memory

import (
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestChainContract(t *testing.T) {
	contract.RunChains(t, func(t *testing.T) store.Chains {
		t.Helper()
		repository, err := NewChains(100)
		if err != nil {
			t.Fatal(err)
		}
		return repository
	})
}
