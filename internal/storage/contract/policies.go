package contract

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func Policy(id string) policy.Policy {
	return policy.Policy{
		Version: 1,
		ID:      model.ID(id),
		Name:    "Default policy",
		Rules: []policy.Rule{{
			ID: "route", Name: "Route traffic", Priority: 100, Enabled: true,
			StopProcessing: true,
			Conditions:     policy.Condition{Field: "host", Operator: "suffix", Values: []string{"example.invalid"}},
			Actions:        []policy.Action{{Type: "proxy", PoolID: "pool"}},
		}},
	}
}

func RunPolicies(t *testing.T, newStore func(*testing.T) store.Policies) {
	t.Helper()
	t.Run("policy CRUD and revisions", func(t *testing.T) {
		repository := newStore(t)
		document := Policy("a")
		if _, err := repository.GetPolicy(t.Context(), document.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		record, err := repository.PutPolicy(t.Context(), document, 0)
		if err != nil || record.Revision != 1 {
			t.Fatal(record, err)
		}
		if _, err = repository.PutPolicy(t.Context(), document, 0); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		document.Name = "Updated policy"
		record, err = repository.PutPolicy(t.Context(), document, 1)
		if err != nil || record.Revision != 2 {
			t.Fatal(record, err)
		}
		if err = repository.DeletePolicy(t.Context(), document.ID, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
		if err = repository.DeletePolicy(t.Context(), document.ID, 2); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("policy copy isolation and pagination", func(t *testing.T) {
		repository := newStore(t)
		document := Policy("b")
		record, err := repository.PutPolicy(t.Context(), document, 0)
		if err != nil {
			t.Fatal(err)
		}
		document.Rules[0].Actions[0].PoolID = "input-mutation"
		record.Policy.Rules[0].Conditions.Values[0] = "output-mutation"
		stored, err := repository.GetPolicy(t.Context(), "b")
		if err != nil || stored.Policy.Rules[0].Actions[0].PoolID != "pool" || stored.Policy.Rules[0].Conditions.Values[0] != "example.invalid" {
			t.Fatal(stored, err)
		}
		for _, id := range []string{"c", "a"} {
			if _, err = repository.PutPolicy(t.Context(), Policy(id), 0); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := repository.ListPolicies(t.Context(), store.Page{Limit: 2})
		if err != nil || len(rows) != 2 || rows[0].Policy.ID != "a" || rows[1].Policy.ID != "b" {
			t.Fatal(rows, err)
		}
		rows[0].Policy.Rules[0].Actions[0].PoolID = "list-mutation"
		stored, _ = repository.GetPolicy(t.Context(), "a")
		if stored.Policy.Rules[0].Actions[0].PoolID != "pool" {
			t.Fatal("list alias")
		}
		rows, err = repository.ListPolicies(t.Context(), store.Page{After: "b", Limit: 2})
		if err != nil || len(rows) != 1 || rows[0].Policy.ID != "c" {
			t.Fatal(rows, err)
		}
	})
	t.Run("policy invalid values and cancellation", func(t *testing.T) {
		repository := newStore(t)
		document := Policy("a")
		document.Version = 2
		if _, err := repository.PutPolicy(t.Context(), document, 0); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := repository.ListPolicies(ctx, store.Page{Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("policy concurrent stale writers", func(t *testing.T) {
		repository := newStore(t)
		document := Policy("a")
		if _, err := repository.PutPolicy(t.Context(), document, 0); err != nil {
			t.Fatal(err)
		}
		var wins atomic.Int32
		var wait sync.WaitGroup
		for range 12 {
			wait.Go(func() {
				_, err := repository.PutPolicy(t.Context(), document, 1)
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
