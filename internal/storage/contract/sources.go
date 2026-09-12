package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func Source(id string) proxy.Source {
	return proxy.Source{
		ID:              model.ID(id),
		Name:            "Fake source",
		Type:            proxy.APISource,
		Config:          map[string]string{"url": "https://source.example.invalid/list"},
		RefreshInterval: time.Hour,
		Enabled:         true,
	}
}

func RunSources(t *testing.T, newStore func(*testing.T) store.Sources) {
	t.Helper()
	t.Run("source CRUD and revisions", func(t *testing.T) {
		repository := newStore(t)
		ctx := t.Context()
		source := Source("a")
		if _, err := repository.GetSource(ctx, source.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		record, err := repository.PutSource(ctx, source, 0)
		if err != nil || record.Revision != 1 {
			t.Fatalf("%+v %v", record, err)
		}
		if _, err = repository.PutSource(ctx, source, 0); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		source.Name = "Updated source"
		record, err = repository.PutSource(ctx, source, 1)
		if err != nil || record.Revision != 2 {
			t.Fatalf("%+v %v", record, err)
		}
		if _, err = repository.PutSource(ctx, source, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = repository.DeleteSource(ctx, source.ID, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = repository.DeleteSource(ctx, source.ID, 2); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("source copy isolation and pagination", func(t *testing.T) {
		repository := newStore(t)
		ctx := t.Context()
		source := Source("b")
		record, err := repository.PutSource(ctx, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		source.Config["url"] = "input mutation"
		record.Source.Config["url"] = "output mutation"
		stored, err := repository.GetSource(ctx, "b")
		if err != nil || stored.Source.Config["url"] != "https://source.example.invalid/list" {
			t.Fatalf("%+v %v", stored, err)
		}
		for _, id := range []string{"c", "a"} {
			if _, err = repository.PutSource(ctx, Source(id), 0); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := repository.ListSources(ctx, store.Page{Limit: 2})
		if err != nil || len(rows) != 2 || rows[0].Source.ID != "a" || rows[1].Source.ID != "b" {
			t.Fatalf("%+v %v", rows, err)
		}
		rows[0].Source.Config["url"] = "list mutation"
		stored, _ = repository.GetSource(ctx, "a")
		if stored.Source.Config["url"] != "https://source.example.invalid/list" {
			t.Fatal("list alias")
		}
		rows, err = repository.ListSources(ctx, store.Page{After: "b", Limit: 2})
		if err != nil || len(rows) != 1 || rows[0].Source.ID != "c" {
			t.Fatalf("%+v %v", rows, err)
		}
	})
	t.Run("source invalid values and cancellation", func(t *testing.T) {
		repository := newStore(t)
		source := Source("a")
		source.Name = ""
		if _, err := repository.PutSource(t.Context(), source, 0); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := repository.ListSources(ctx, store.Page{Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("source concurrent stale writers", func(t *testing.T) {
		repository := newStore(t)
		source := Source("a")
		if _, err := repository.PutSource(t.Context(), source, 0); err != nil {
			t.Fatal(err)
		}
		var wins atomic.Int32
		var wait sync.WaitGroup
		for range 12 {
			wait.Go(func() {
				_, err := repository.PutSource(t.Context(), source, 1)
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
