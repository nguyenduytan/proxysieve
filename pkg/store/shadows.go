package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/shadow"
)

type ShadowRecord struct {
	Shadow   shadow.Config `json:"shadow"`
	Revision int64         `json:"revision"`
}

type Shadows interface {
	GetShadow(context.Context, model.ID) (ShadowRecord, error)
	PutShadow(context.Context, shadow.Config, int64) (ShadowRecord, error)
	DeleteShadow(context.Context, model.ID, int64) error
	ListShadows(context.Context) ([]ShadowRecord, error)
}
