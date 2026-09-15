package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func Chain(id string) routing.Chain {
	return routing.Chain{ID: model.ID(id), Name: "Ordered chain", Hops: []routing.Hop{{PoolID: "first", Timeout: time.Second}, {PoolID: "second", Timeout: 2 * time.Second}}, Enabled: true}
}

func RunChains(t *testing.T, newStore func(*testing.T) store.Chains) {
	t.Helper()
	t.Run("chain CRUD revisions and copy isolation", func(t *testing.T) {
		repository := newStore(t)
		chain := Chain("a")
		if _, err := repository.GetChain(t.Context(), chain.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		record, err := repository.PutChain(t.Context(), chain, 0)
		if err != nil || record.Revision != 1 {
			t.Fatal(record, err)
		}
		chain.Hops[0].PoolID = "mutated"
		record.Chain.Hops[0].PoolID = "mutated-output"
		stored, err := repository.GetChain(t.Context(), "a")
		if err != nil || stored.Chain.Hops[0].PoolID != "first" {
			t.Fatal(stored, err)
		}
		updated := stored.Chain
		updated.Name = "Updated"
		if _, err = repository.PutChain(t.Context(), updated, 0); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		record, err = repository.PutChain(t.Context(), updated, 1)
		if err != nil || record.Revision != 2 {
			t.Fatal(record, err)
		}
		if err = repository.DeleteChain(t.Context(), "a", 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = repository.DeleteChain(t.Context(), "a", 2); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("chain pagination validation and concurrency", func(t *testing.T) {
		repository := newStore(t)
		for _, id := range []string{"b", "c", "a"} {
			if _, err := repository.PutChain(t.Context(), Chain(id), 0); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := repository.ListChains(t.Context(), store.Page{Limit: 2})
		if err != nil || len(rows) != 2 || rows[0].Chain.ID != "a" || rows[1].Chain.ID != "b" {
			t.Fatal(rows, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err = repository.ListChains(ctx, store.Page{Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		chain := Chain("d")
		if _, err = repository.PutChain(t.Context(), chain, 0); err != nil {
			t.Fatal(err)
		}
		var wins atomic.Int32
		var wait sync.WaitGroup
		for range 12 {
			wait.Go(func() {
				if _, putErr := repository.PutChain(t.Context(), chain, 1); putErr == nil {
					wins.Add(1)
				} else if !errors.Is(putErr, store.ErrConflict) {
					t.Error(putErr)
				}
			})
		}
		wait.Wait()
		if wins.Load() != 1 {
			t.Fatalf("winners=%d", wins.Load())
		}
	})
}
