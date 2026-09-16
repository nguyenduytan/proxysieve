package sqlite

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func TestBudgetInventorySeedsOnceAndKeepsEmptyDeletion(t *testing.T) {
	repository, err := Open(t.Context(), filepath.Join(t.TempDir(), "budget-inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	configured := budget.Config{ID: "system", Name: "System", Scope: budget.ScopeSystem, Limit: 100, Hard: true, Action: budget.ActionReject}
	records, err := repository.InitializeBudgets(t.Context(), []budget.Config{configured})
	if err != nil || len(records) != 1 || records[0].Revision != 1 {
		t.Fatal(records, err)
	}
	configured.Name = "Updated"
	record, err := repository.PutBudget(t.Context(), configured, 1)
	if err != nil || record.Revision != 2 {
		t.Fatal(record, err)
	}
	if _, err = repository.PutBudget(t.Context(), configured, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if err = repository.DeleteBudget(t.Context(), configured.ID, 2); err != nil {
		t.Fatal(err)
	}
	records, err = repository.InitializeBudgets(t.Context(), []budget.Config{{ID: "replacement", Name: "Replacement", Limit: 1, Hard: true, Action: budget.ActionReject}})
	if err != nil || len(records) != 0 {
		t.Fatal(records, err)
	}
}

func TestBudgetInventoryConcurrentInitializationHasOneSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-budget-inventory.db")
	first, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	stores := []*Store{first, second}
	ids := []string{"first", "second"}
	results := make([][]store.BudgetRecord, 2)
	errorsSeen := make([]error, 2)
	ready := make(chan struct{})
	var group sync.WaitGroup
	for index := range stores {
		group.Go(func() {
			<-ready
			results[index], errorsSeen[index] = stores[index].InitializeBudgets(t.Context(), []budget.Config{{ID: model.ID(ids[index]), Name: ids[index], Limit: 1, Hard: true, Action: budget.ActionReject}})
		})
	}
	close(ready)
	group.Wait()
	if errorsSeen[0] != nil || errorsSeen[1] != nil || len(results[0]) != 1 || len(results[1]) != 1 || results[0][0].Budget.ID != results[1][0].Budget.ID {
		t.Fatal(results, errorsSeen)
	}
}
