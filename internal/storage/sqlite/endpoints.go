package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"math"
)

type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type endpoints struct{ q querier }

func (s *endpoints) Get(ctx context.Context, id model.ID) (store.EndpointRecord, error) {
	if !id.Valid() {
		return store.EndpointRecord{}, store.ErrInvalid
	}
	var b []byte
	var r store.EndpointRecord
	err := s.q.QueryRowContext(ctx, "SELECT document,revision FROM proxy_endpoints WHERE id=?", string(id)).Scan(&b, &r.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return r, store.ErrNotFound
	}
	if err != nil {
		return r, safeError(ctx, err)
	}
	if json.Unmarshal(b, &r.Endpoint) != nil || r.Endpoint.Validate() != nil || r.Endpoint.ID != id {
		return store.EndpointRecord{}, store.ErrSchema
	}
	return r, nil
}
func (s *endpoints) Put(ctx context.Context, e proxy.Endpoint, expected int64) (store.EndpointRecord, error) {
	if e.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.EndpointRecord{}, store.ErrInvalid
	}
	b, err := json.Marshal(e)
	if err != nil || len(b) > 1<<20 {
		return store.EndpointRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.q.ExecContext(ctx, "INSERT INTO proxy_endpoints(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(e.ID), string(b))
	} else {
		result, err = s.q.ExecContext(ctx, "UPDATE proxy_endpoints SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(b), string(e.ID), expected)
	}
	if err != nil {
		return store.EndpointRecord{}, safeError(ctx, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return store.EndpointRecord{}, safeError(ctx, err)
	}
	if n == 0 {
		if expected > 0 {
			if _, err = s.Get(ctx, e.ID); err != nil {
				return store.EndpointRecord{}, err
			}
		}
		return store.EndpointRecord{}, store.ErrConflict
	}
	return store.EndpointRecord{Endpoint: e.Clone(), Revision: expected + 1}, nil
}
func (s *endpoints) Delete(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.q.ExecContext(ctx, "DELETE FROM proxy_endpoints WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if n == 0 {
		if _, err = s.Get(ctx, id); err != nil {
			return err
		}
		return store.ErrConflict
	}
	return nil
}
func (s *endpoints) List(ctx context.Context, p store.Page) ([]store.EndpointRecord, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, "SELECT id,document,revision FROM proxy_endpoints WHERE id>? ORDER BY id LIMIT ?", string(p.After), p.Limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]store.EndpointRecord, 0, p.Limit)
	for rows.Next() {
		var id string
		var b []byte
		var r store.EndpointRecord
		if err = rows.Scan(&id, &b, &r.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(b, &r.Endpoint) != nil || r.Endpoint.Validate() != nil || string(r.Endpoint.ID) != id {
			return nil, store.ErrSchema
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return out, nil
}
