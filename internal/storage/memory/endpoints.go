// Package memory supplies bounded, isolated repositories for tests and ephemeral mode.
package memory

import (
	"context"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"math"
	"sort"
	"sync"
)

type Endpoints struct {
	mu   sync.RWMutex
	rows map[model.ID]store.EndpointRecord
	max  int
}

func NewEndpoints(max int) (*Endpoints, error) {
	if max < 1 || max > 1_000_000 {
		return nil, store.ErrInvalid
	}
	return &Endpoints{rows: map[model.ID]store.EndpointRecord{}, max: max}, nil
}
func (s *Endpoints) Get(ctx context.Context, id model.ID) (store.EndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return (&transaction{rows: s.rows, max: s.max}).Get(ctx, id)
}
func (s *Endpoints) Put(ctx context.Context, e proxy.Endpoint, expected int64) (store.EndpointRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&transaction{rows: s.rows, max: s.max}).Put(ctx, e, expected)
}
func (s *Endpoints) Delete(ctx context.Context, id model.ID, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&transaction{rows: s.rows, max: s.max}).Delete(ctx, id, expected)
}
func (s *Endpoints) List(ctx context.Context, p store.Page) ([]store.EndpointRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return (&transaction{rows: s.rows, max: s.max}).List(ctx, p)
}
func (s *Endpoints) WithinTransaction(ctx context.Context, fn func(store.Endpoints) error) error {
	if fn == nil {
		return store.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make(map[model.ID]store.EndpointRecord, len(s.rows))
	for id, r := range s.rows {
		r.Endpoint = r.Endpoint.Clone()
		rows[id] = r
	}
	tx := &transaction{rows: rows, max: s.max}
	defer func() { tx.closed = true }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.rows = rows
	return nil
}

type transaction struct {
	rows   map[model.ID]store.EndpointRecord
	max    int
	closed bool
}

func (t *transaction) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.closed {
		return store.ErrUnavailable
	}
	return nil
}
func (t *transaction) Get(ctx context.Context, id model.ID) (store.EndpointRecord, error) {
	if err := t.ready(ctx); err != nil {
		return store.EndpointRecord{}, err
	}
	if !id.Valid() {
		return store.EndpointRecord{}, store.ErrInvalid
	}
	r, ok := t.rows[id]
	if !ok {
		return store.EndpointRecord{}, store.ErrNotFound
	}
	r.Endpoint = r.Endpoint.Clone()
	return r, nil
}
func (t *transaction) Put(ctx context.Context, e proxy.Endpoint, expected int64) (store.EndpointRecord, error) {
	if err := t.ready(ctx); err != nil {
		return store.EndpointRecord{}, err
	}
	if e.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.EndpointRecord{}, store.ErrInvalid
	}
	current, exists := t.rows[e.ID]
	if expected > 0 && !exists {
		return store.EndpointRecord{}, store.ErrNotFound
	}
	if exists && current.Revision != expected {
		return store.EndpointRecord{}, store.ErrConflict
	}
	if !exists && len(t.rows) >= t.max {
		return store.EndpointRecord{}, store.ErrUnavailable
	}
	r := store.EndpointRecord{Endpoint: e.Clone(), Revision: expected + 1}
	t.rows[e.ID] = r
	r.Endpoint = r.Endpoint.Clone()
	return r, nil
}
func (t *transaction) Delete(ctx context.Context, id model.ID, expected int64) error {
	if expected < 1 {
		return store.ErrInvalid
	}
	r, err := t.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.Revision != expected {
		return store.ErrConflict
	}
	delete(t.rows, id)
	return nil
}
func (t *transaction) List(ctx context.Context, p store.Page) ([]store.EndpointRecord, error) {
	if err := t.ready(ctx); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(t.rows))
	for id := range t.rows {
		if id > p.After {
			ids = append(ids, string(id))
		}
	}
	sort.Strings(ids)
	if len(ids) > p.Limit {
		ids = ids[:p.Limit]
	}
	rows := make([]store.EndpointRecord, 0, len(ids))
	for _, id := range ids {
		r := t.rows[model.ID(id)]
		r.Endpoint = r.Endpoint.Clone()
		rows = append(rows, r)
	}
	return rows, nil
}

var _ store.EndpointStore = (*Endpoints)(nil)
