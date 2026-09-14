package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
)

type PoolRecord struct {
	Pool     routing.Pool `json:"pool"`
	Revision int64        `json:"revision"`
}

// Pools persists revisioned routing-pool inventory. Persisted changes do not
// alter the immutable runtime configuration until an explicit activation flow
// is implemented.
type Pools interface {
	GetPool(context.Context, model.ID) (PoolRecord, error)
	PutPool(context.Context, routing.Pool, int64) (PoolRecord, error)
	DeletePool(context.Context, model.ID, int64) error
	ListPools(context.Context, Page) ([]PoolRecord, error)
}
