// Package contract provides shared behavioral tests for endpoint storage adapters.
package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func Endpoint(id string) proxy.Endpoint {
	return proxy.Endpoint{ID: model.ID(id), Name: "Fake proxy", Protocol: proxy.HTTP, Host: "example.invalid", Port: 8080, Enabled: true, Tags: []string{"test"}, Metadata: map[string]string{"country": "ca"}}
}

func Run(t *testing.T, newStore func(*testing.T) store.EndpointStore) {
	t.Helper()
	t.Run("CRUD and revisions", func(t *testing.T) {
		s := newStore(t)
		ctx := t.Context()
		e := Endpoint("a")
		if _, err := s.Get(ctx, e.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		r, err := s.Put(ctx, e, 0)
		if err != nil || r.Revision != 1 {
			t.Fatalf("%+v %v", r, err)
		}
		if _, err = s.Put(ctx, e, 0); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		e.Name = "updated"
		r, err = s.Put(ctx, e, 1)
		if err != nil || r.Revision != 2 {
			t.Fatalf("%+v %v", r, err)
		}
		if _, err = s.Put(ctx, e, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = s.Delete(ctx, e.ID, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = s.Delete(ctx, e.ID, 2); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Put(ctx, e, 2); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		if err = s.Delete(ctx, e.ID, 2); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("copy isolation and pagination", func(t *testing.T) {
		s := newStore(t)
		ctx := t.Context()
		e := Endpoint("b")
		r, err := s.Put(ctx, e, 0)
		if err != nil {
			t.Fatal(err)
		}
		e.Tags[0] = "input mutation"
		r.Endpoint.Metadata["country"] = "output mutation"
		got, err := s.Get(ctx, "b")
		if err != nil || got.Endpoint.Tags[0] != "test" || got.Endpoint.Metadata["country"] != "ca" {
			t.Fatalf("%+v %v", got, err)
		}
		for _, id := range []string{"c", "a"} {
			if _, err = s.Put(ctx, Endpoint(id), 0); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := s.List(ctx, store.Page{Limit: 2})
		if err != nil || len(rows) != 2 || rows[0].Endpoint.ID != "a" || rows[1].Endpoint.ID != "b" {
			t.Fatalf("%+v %v", rows, err)
		}
		rows[0].Endpoint.Tags[0] = "mutated"
		got, _ = s.Get(ctx, "a")
		if got.Endpoint.Tags[0] != "test" {
			t.Fatal("list alias")
		}
		rows, err = s.List(ctx, store.Page{After: "b", Limit: 2})
		if err != nil || len(rows) != 1 || rows[0].Endpoint.ID != "c" {
			t.Fatalf("%+v %v", rows, err)
		}
		for _, p := range []store.Page{{}, {Limit: 1001}, {Limit: 1, After: "../../"}} {
			if _, err = s.List(ctx, p); !errors.Is(err, store.ErrInvalid) {
				t.Fatal(err)
			}
		}
	})
	t.Run("transaction rollback commit cancel lifetime", func(t *testing.T) {
		s := newStore(t)
		ctx := t.Context()
		boom := errors.New("rollback")
		err := s.WithinTransaction(ctx, func(tx store.Endpoints) error {
			if _, e := tx.Put(ctx, Endpoint("a"), 0); e != nil {
				t.Fatal(e)
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatal(err)
		}
		if _, err = s.Get(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("rollback failed", err)
		}
		var retained store.Endpoints
		err = s.WithinTransaction(ctx, func(tx store.Endpoints) error { retained = tx; _, e := tx.Put(ctx, Endpoint("a"), 0); return e })
		if err != nil {
			t.Fatal(err)
		}
		if _, err = retained.Get(ctx, "a"); !errors.Is(err, store.ErrUnavailable) {
			t.Fatal("expired transaction usable", err)
		}
		if _, err = s.Get(ctx, "a"); err != nil {
			t.Fatal(err)
		}
		cancelCtx, cancel := context.WithCancel(ctx)
		err = s.WithinTransaction(cancelCtx, func(tx store.Endpoints) error { _, e := tx.Put(cancelCtx, Endpoint("b"), 0); cancel(); return e })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err = s.Get(ctx, "b"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("canceled commit", err)
		}
	})
	t.Run("concurrent stale writers", func(t *testing.T) {
		s := newStore(t)
		ctx := t.Context()
		e := Endpoint("a")
		if _, err := s.Put(ctx, e, 0); err != nil {
			t.Fatal(err)
		}
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 12 {
			wg.Go(func() {
				_, err := s.Put(ctx, e, 1)
				if err == nil {
					wins.Add(1)
				} else if !errors.Is(err, store.ErrConflict) {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatalf("winners=%d", wins.Load())
		}
	})
	t.Run("invalid values and cancellation", func(t *testing.T) {
		s := newStore(t)
		ctx := t.Context()
		e := Endpoint("a")
		e.Host = "http://fake:password@example.invalid"
		if _, err := s.Put(ctx, e, 0); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, "../secret"); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, "a", 0); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.Get(c, "a"); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := s.Put(c, Endpoint("a"), 0); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := s.List(c, store.Page{Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
