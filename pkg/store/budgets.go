package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
)

type BudgetRecord struct {
	Budget   budget.Config `json:"budget"`
	Revision int64         `json:"revision"`
}

type Budgets interface {
	InitializeBudgets(context.Context, []budget.Config) ([]BudgetRecord, error)
	GetBudget(context.Context, model.ID) (BudgetRecord, error)
	PutBudget(context.Context, budget.Config, int64) (BudgetRecord, error)
	DeleteBudget(context.Context, model.ID, int64) error
	ListBudgets(context.Context) ([]BudgetRecord, error)
}
