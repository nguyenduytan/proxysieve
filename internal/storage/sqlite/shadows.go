package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/shadow"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func (s *Store) GetShadow(ctx context.Context, id model.ID) (store.ShadowRecord, error) {
	if !id.Valid() {
		return store.ShadowRecord{}, store.ErrInvalid
	}
	var document []byte
	var record store.ShadowRecord
	err := s.db.QueryRowContext(ctx, "SELECT document,revision FROM shadow_policies WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Shadow) != nil || record.Shadow.Validate() != nil || record.Shadow.ID != id {
		return store.ShadowRecord{}, store.ErrSchema
	}
	return record, nil
}

func (s *Store) PutShadow(ctx context.Context, config shadow.Config, expected int64) (store.ShadowRecord, error) {
	if config.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.ShadowRecord{}, store.ErrInvalid
	}
	document, err := json.Marshal(config)
	if err != nil || len(document) > 1<<20 {
		return store.ShadowRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO shadow_policies(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(config.ID), string(document))
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE shadow_policies SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(config.ID), expected)
	}
	if err != nil {
		return store.ShadowRecord{}, safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return store.ShadowRecord{}, safeError(ctx, err)
	}
	if changed == 0 {
		if expected > 0 {
			if _, err = s.GetShadow(ctx, config.ID); err != nil {
				return store.ShadowRecord{}, err
			}
		}
		return store.ShadowRecord{}, store.ErrConflict
	}
	return store.ShadowRecord{Shadow: config.Clone(), Revision: expected + 1}, nil
}

func (s *Store) DeleteShadow(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM shadow_policies WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if changed == 1 {
		return nil
	}
	if _, err = s.GetShadow(ctx, id); err != nil {
		return err
	}
	return store.ErrConflict
}

func (s *Store) ListShadows(ctx context.Context) ([]store.ShadowRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,document,revision FROM shadow_policies ORDER BY id")
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := []store.ShadowRecord{}
	for rows.Next() {
		var id model.ID
		var document []byte
		var record store.ShadowRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Shadow) != nil || record.Shadow.Validate() != nil || record.Shadow.ID != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

var _ store.Shadows = (*Store)(nil)
