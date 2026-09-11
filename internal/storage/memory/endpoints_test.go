package memory

import (
	"github.com/nguyenduytan/proxysieve/internal/storage/contract"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"testing"
)

func TestContract(t *testing.T) {
	contract.Run(t, func(t *testing.T) store.EndpointStore {
		t.Helper()
		s, err := NewEndpoints(100)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}
func TestCapacity(t *testing.T) {
	s, err := NewEndpoints(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(t.Context(), contract.Endpoint("a"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(t.Context(), contract.Endpoint("b"), 0); err == nil {
		t.Fatal("capacity exceeded")
	}
	if _, err = s.Put(t.Context(), contract.Endpoint("a"), 1); err != nil {
		t.Fatal("update at capacity failed", err)
	}
}
