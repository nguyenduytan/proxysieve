package budget

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"sync"
	"testing"
)

func TestReservationsAreAtomicAndReconciled(t *testing.T) {
	m, err := New([]budget.Config{{ID: "system", Name: "System", Limit: 100, Hard: true, Action: budget.ActionReject}, {ID: "client", Name: "Client", Limit: 100, Hard: true, Action: budget.ActionReject}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Reserve(context.Background(), []model.ID{"system", "client"}, 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Reserve(context.Background(), []model.ID{"system", "client"}, 50); !errors.Is(err, budget.ErrExceeded) {
		t.Fatal(err)
	}
	allowed, err := first.Consume(40)
	if err != nil || allowed != 40 {
		t.Fatal(allowed, err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"system", "client"} {
		usage, _ := m.Usage(model.ID(id))
		if usage.Used != 40 || usage.Reserved != 0 {
			t.Fatal(id, usage)
		}
	}
}
func TestConcurrentReservationsCannotOverspend(t *testing.T) {
	m, _ := New([]budget.Config{{ID: "system", Name: "System", Limit: 1000, Hard: true, Action: budget.ActionReject}}, 10)
	var group sync.WaitGroup
	var granted int
	var lock sync.Mutex
	for range 200 {
		group.Go(func() {
			lease, err := m.Reserve(context.Background(), []model.ID{"system"}, 10)
			if err == nil {
				_, _ = lease.Consume(10)
				_ = lease.Close()
				lock.Lock()
				granted += 10
				lock.Unlock()
			}
		})
	}
	group.Wait()
	usage, _ := m.Usage("system")
	if granted != 1000 || usage.Used != 1000 || usage.Reserved != 0 {
		t.Fatal(granted, usage)
	}
}
func TestLeaseNeverGrantsPastReservation(t *testing.T) {
	m, _ := New([]budget.Config{{ID: "system", Name: "System", Limit: 20, Hard: true, Action: budget.ActionReject}}, 20)
	lease, _ := m.Reserve(context.Background(), []model.ID{"system"}, 10)
	allowed, err := lease.Consume(15)
	if !errors.Is(err, budget.ErrExceeded) || allowed != 10 {
		t.Fatal(allowed, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Consume(1); !errors.Is(err, budget.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := m.Reserve(context.Background(), []model.ID{"system"}, 11); !errors.Is(err, budget.ErrExceeded) {
		t.Fatal(err)
	}
	_ = traffic.Bytes(0)
}
