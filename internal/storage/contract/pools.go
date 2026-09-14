package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func Pool(id string) routing.Pool {
	return routing.Pool{ID: model.ID(id), Name: "Primary pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, RequiredTags: []string{"residential"}, Enabled: true}
}

func RunPools(t *testing.T, newStore func(*testing.T) store.Pools) {
	t.Helper()
	t.Run("pool CRUD and revisions", func(t *testing.T) {
		repository := newStore(t)
		pool := Pool("a")
		if _, err := repository.GetPool(t.Context(), pool.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		record, err := repository.PutPool(t.Context(), pool, 0)
		if err != nil || record.Revision != 1 {
			t.Fatal(record, err)
		}
		if _, err = repository.PutPool(t.Context(), pool, 0); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		pool.Name = "Updated pool"
		record, err = repository.PutPool(t.Context(), pool, 1)
		if err != nil || record.Revision != 2 {
			t.Fatal(record, err)
		}
		if err = repository.DeletePool(t.Context(), pool.ID, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = repository.DeletePool(t.Context(), pool.ID, 2); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("pool copy isolation and pagination", func(t *testing.T) {
		repository := newStore(t)
		pool := Pool("b")
		record, err := repository.PutPool(t.Context(), pool, 0)
		if err != nil {
			t.Fatal(err)
		}
		pool.EndpointIDs[0] = "input-mutation"
		record.Pool.RequiredTags[0] = "output-mutation"
		stored, err := repository.GetPool(t.Context(), "b")
		if err != nil || stored.Pool.EndpointIDs[0] != "proxy" || stored.Pool.RequiredTags[0] != "residential" {
			t.Fatal(stored, err)
		}
		for _, id := range []string{"c", "a"} {
			if _, err = repository.PutPool(t.Context(), Pool(id), 0); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := repository.ListPools(t.Context(), store.Page{Limit: 2})
		if err != nil || len(rows) != 2 || rows[0].Pool.ID != "a" || rows[1].Pool.ID != "b" {
			t.Fatal(rows, err)
		}
		rows, err = repository.ListPools(t.Context(), store.Page{After: "b", Limit: 2})
		if err != nil || len(rows) != 1 || rows[0].Pool.ID != "c" {
			t.Fatal(rows, err)
		}
	})
	t.Run("pool invalid values and cancellation", func(t *testing.T) {
		repository := newStore(t)
		pool := Pool("a")
		pool.Strategy = "unknown"
		if _, err := repository.PutPool(t.Context(), pool, 0); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := repository.ListPools(ctx, store.Page{Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("pool concurrent stale writers", func(t *testing.T) {
		repository := newStore(t)
		pool := Pool("a")
		if _, err := repository.PutPool(t.Context(), pool, 0); err != nil {
			t.Fatal(err)
		}
		var wins atomic.Int32
		var wait sync.WaitGroup
		for range 12 {
			wait.Go(func() {
				_, err := repository.PutPool(t.Context(), pool, 1)
				if err == nil {
					wins.Add(1)
				} else if !errors.Is(err, store.ErrConflict) {
					t.Error(err)
				}
			})
		}
		wait.Wait()
		if wins.Load() != 1 {
			t.Fatalf("winners=%d", wins.Load())
		}
	})
}
