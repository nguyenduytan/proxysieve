package store

import (
	"context"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

type SourceRecord struct {
	Source   proxy.Source `json:"source"`
	Revision int64        `json:"revision"`
}

// Sources persists proxy-source definitions independently from endpoint
// inventory. Refresh status is part of the revisioned source document.
type Sources interface {
	GetSource(context.Context, model.ID) (SourceRecord, error)
	// PutSource creates with expected=0, or updates only the specified positive revision.
	PutSource(context.Context, proxy.Source, int64) (SourceRecord, error)
	DeleteSource(context.Context, model.ID, int64) error
	ListSources(context.Context, Page) ([]SourceRecord, error)
}
