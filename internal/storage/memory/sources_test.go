package memory

import (
	"errors"
	"testing"

	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestSourceContract(t *testing.T) {
	contract.RunSources(t, func(t *testing.T) store.Sources {
		t.Helper()
		repository, err := NewSources(100)
		if err != nil {
			t.Fatal(err)
		}
		return repository
	})
}

func TestSourceCapacity(t *testing.T) {
	repository, err := NewSources(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.PutSource(t.Context(), contract.Source("a"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.PutSource(t.Context(), contract.Source("b"), 0); !errors.Is(err, store.ErrUnavailable) {
		t.Fatal(err)
	}
}
