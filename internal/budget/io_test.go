package budget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

func TestBudgetedReaderUsesRemainingAllowanceThenStops(t *testing.T) {
	manager, err := New([]publicbudget.Config{{ID: "system", Name: "System", Limit: 5, Hard: true, Action: publicbudget.ActionReject}}, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(ctx context.Context, amount traffic.Bytes) (publicbudget.Lease, error) {
		return manager.Reserve(ctx, []model.ID{"system"}, amount)
	}
	reader := &Reader{Context: t.Context(), Source: strings.NewReader("123456789"), Reserve: reserve}
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "12345" {
		t.Fatal(string(got), err)
	}
	usage, _ := manager.Usage("system")
	if usage.Used != 5 || usage.Reserved != 0 {
		t.Fatal(usage)
	}
}

func TestBudgetedWriterUsesRemainingAllowanceThenStops(t *testing.T) {
	manager, err := New([]publicbudget.Config{{ID: "system", Name: "System", Limit: 5, Hard: true, Action: publicbudget.ActionReject}}, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(ctx context.Context, amount traffic.Bytes) (publicbudget.Lease, error) {
		return manager.Reserve(ctx, []model.ID{"system"}, amount)
	}
	var destination bytes.Buffer
	writer := &Writer{Context: t.Context(), Destination: &destination, Reserve: reserve}
	n, err := writer.Write([]byte("123456789"))
	if !errors.Is(err, publicbudget.ErrExceeded) || n != 5 || destination.String() != "12345" {
		t.Fatal(n, destination.String(), err)
	}
}
