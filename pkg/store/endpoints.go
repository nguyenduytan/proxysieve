// Package store defines storage-independent persistence boundaries and errors.
package store

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

var (
	ErrNotFound    = errors.New("record not found")
	ErrConflict    = errors.New("record revision conflict")
	ErrUnavailable = errors.New("storage unavailable")
	ErrInvalid     = errors.New("invalid storage request")
	ErrSchema      = errors.New("unsupported or modified database schema")
)

type EndpointRecord struct {
	Endpoint proxy.Endpoint `json:"endpoint"`
	Revision int64          `json:"revision"`
}

// Page uses stable ID keyset pagination; limit must be 1..1000.
type Page struct {
	After model.ID
	Limit int
}

func (p Page) Validate() error {
	if p.Limit < 1 || p.Limit > 1000 || p.After != "" && !p.After.Valid() {
		return ErrInvalid
	}
	return nil
}

type Endpoints interface {
	Get(context.Context, model.ID) (EndpointRecord, error)
	// Put creates with expected=0, or updates only the specified positive revision.
	Put(context.Context, proxy.Endpoint, int64) (EndpointRecord, error)
	Delete(context.Context, model.ID, int64) error
	List(context.Context, Page) ([]EndpointRecord, error)
}
type EndpointStore interface {
	Endpoints
	// WithinTransaction commits only on nil callback error and live context. The
	// callback must use the supplied repository, not re-enter the parent store,
	// and must not retain it or use it concurrently/after returning.
	WithinTransaction(context.Context, func(Endpoints) error) error
}
