package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
)

type ChainRecord struct {
	Chain    routing.Chain `json:"chain"`
	Revision int64         `json:"revision"`
}

type Chains interface {
	GetChain(context.Context, model.ID) (ChainRecord, error)
	PutChain(context.Context, routing.Chain, int64) (ChainRecord, error)
	DeleteChain(context.Context, model.ID, int64) error
	ListChains(context.Context, Page) ([]ChainRecord, error)
}
