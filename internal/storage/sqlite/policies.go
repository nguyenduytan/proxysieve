package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type policies struct{ q querier }

func (s *policies) GetPolicy(ctx context.Context, id model.ID) (store.PolicyRecord, error) {
	if !id.Valid() {
		return store.PolicyRecord{}, store.ErrInvalid
	}
	var document []byte
	var record store.PolicyRecord
	err := s.q.QueryRowContext(ctx, "SELECT document,revision FROM policies WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Policy) != nil || record.Policy.Validate() != nil || record.Policy.ID != id {
		return store.PolicyRecord{}, store.ErrSchema
	}
	return record, nil
}

func (s *policies) PutPolicy(ctx context.Context, document policy.Policy, expected int64) (store.PolicyRecord, error) {
	if document.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.PolicyRecord{}, store.ErrInvalid
	}
	body, err := json.Marshal(document)
	if err != nil || len(body) > 1<<20 {
		return store.PolicyRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.q.ExecContext(ctx, "INSERT INTO policies(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(document.ID), string(body))
	} else {
		result, err = s.q.ExecContext(ctx, "UPDATE policies SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(body), string(document.ID), expected)
	}
	if err != nil {
		return store.PolicyRecord{}, safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return store.PolicyRecord{}, safeError(ctx, err)
	}
	if rows == 0 {
		if expected > 0 {
			if _, err = s.GetPolicy(ctx, document.ID); err != nil {
				return store.PolicyRecord{}, err
			}
		}
		return store.PolicyRecord{}, store.ErrConflict
	}
	return store.PolicyRecord{Policy: document.Clone(), Revision: expected + 1}, nil
}

func (s *policies) DeletePolicy(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.q.ExecContext(ctx, "DELETE FROM policies WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if rows == 0 {
		if _, err = s.GetPolicy(ctx, id); err != nil {
			return err
		}
		return store.ErrConflict
	}
	return nil
}

func (s *policies) ListPolicies(ctx context.Context, page store.Page) ([]store.PolicyRecord, error) {
	if err := page.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, "SELECT id,document,revision FROM policies WHERE id>? ORDER BY id LIMIT ?", string(page.After), page.Limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]store.PolicyRecord, 0, page.Limit)
	for rows.Next() {
		var id string
		var body []byte
		var record store.PolicyRecord
		if err = rows.Scan(&id, &body, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(body, &record.Policy) != nil || record.Policy.Validate() != nil || string(record.Policy.ID) != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}
