package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type pools struct{ q querier }

func (s *pools) GetPool(ctx context.Context, id model.ID) (store.PoolRecord, error) {
	if !id.Valid() {
		return store.PoolRecord{}, store.ErrInvalid
	}
	var document []byte
	var record store.PoolRecord
	err := s.q.QueryRowContext(ctx, "SELECT document,revision FROM proxy_pools WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Pool) != nil || record.Pool.Validate() != nil || record.Pool.ID != id {
		return store.PoolRecord{}, store.ErrSchema
	}
	return record, nil
}

func (s *pools) PutPool(ctx context.Context, pool routing.Pool, expected int64) (store.PoolRecord, error) {
	if pool.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.PoolRecord{}, store.ErrInvalid
	}
	document, err := json.Marshal(pool)
	if err != nil || len(document) > 1<<20 {
		return store.PoolRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.q.ExecContext(ctx, "INSERT INTO proxy_pools(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(pool.ID), string(document))
	} else {
		result, err = s.q.ExecContext(ctx, "UPDATE proxy_pools SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(pool.ID), expected)
	}
	if err != nil {
		return store.PoolRecord{}, safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return store.PoolRecord{}, safeError(ctx, err)
	}
	if rows == 0 {
		if expected > 0 {
			if _, err = s.GetPool(ctx, pool.ID); err != nil {
				return store.PoolRecord{}, err
			}
		}
		return store.PoolRecord{}, store.ErrConflict
	}
	return store.PoolRecord{Pool: pool.Clone(), Revision: expected + 1}, nil
}

func (s *pools) DeletePool(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.q.ExecContext(ctx, "DELETE FROM proxy_pools WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if rows == 0 {
		if _, err = s.GetPool(ctx, id); err != nil {
			return err
		}
		return store.ErrConflict
	}
	return nil
}

func (s *pools) ListPools(ctx context.Context, page store.Page) ([]store.PoolRecord, error) {
	if err := page.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, "SELECT id,document,revision FROM proxy_pools WHERE id>? ORDER BY id LIMIT ?", string(page.After), page.Limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]store.PoolRecord, 0, page.Limit)
	for rows.Next() {
		var id string
		var document []byte
		var record store.PoolRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Pool) != nil || record.Pool.Validate() != nil || string(record.Pool.ID) != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}
