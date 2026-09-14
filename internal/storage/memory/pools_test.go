package memory

import (
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestPoolContract(t *testing.T) {
	contract.RunPools(t, func(t *testing.T) store.Pools {
		t.Helper()
		repository, err := NewPools(100)
		if err != nil {
			t.Fatal(err)
		}
		return repository
	})
}
