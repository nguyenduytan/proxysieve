package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
)

type PolicyRecord struct {
	Policy   policy.Policy `json:"policy"`
	Revision int64         `json:"revision"`
}

// Policies persists revisioned policy inventory. Persisted documents remain
// separate from the immutable runtime snapshot until explicit activation exists.
type Policies interface {
	GetPolicy(context.Context, model.ID) (PolicyRecord, error)
	PutPolicy(context.Context, policy.Policy, int64) (PolicyRecord, error)
	DeletePolicy(context.Context, model.ID, int64) error
	ListPolicies(context.Context, Page) ([]PolicyRecord, error)
}
