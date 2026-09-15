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

type chains struct{ q querier }

func (s *chains) GetChain(ctx context.Context, id model.ID) (store.ChainRecord, error) {
	if !id.Valid() {
		return store.ChainRecord{}, store.ErrInvalid
	}
	var document []byte
	var record store.ChainRecord
	err := s.q.QueryRowContext(ctx, "SELECT document,revision FROM proxy_chains WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Chain) != nil || record.Chain.Validate() != nil || record.Chain.ID != id {
		return store.ChainRecord{}, store.ErrSchema
	}
	return record, nil
}

func (s *chains) PutChain(ctx context.Context, chain routing.Chain, expected int64) (store.ChainRecord, error) {
	if chain.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.ChainRecord{}, store.ErrInvalid
	}
	document, err := json.Marshal(chain)
	if err != nil || len(document) > 1<<20 {
		return store.ChainRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.q.ExecContext(ctx, "INSERT INTO proxy_chains(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(chain.ID), string(document))
	} else {
		result, err = s.q.ExecContext(ctx, "UPDATE proxy_chains SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(chain.ID), expected)
	}
	if err != nil {
		return store.ChainRecord{}, safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return store.ChainRecord{}, safeError(ctx, err)
	}
	if rows == 0 {
		if expected > 0 {
			if _, err = s.GetChain(ctx, chain.ID); err != nil {
				return store.ChainRecord{}, err
			}
		}
		return store.ChainRecord{}, store.ErrConflict
	}
	return store.ChainRecord{Chain: chain.Clone(), Revision: expected + 1}, nil
}

func (s *chains) DeleteChain(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.q.ExecContext(ctx, "DELETE FROM proxy_chains WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if rows == 0 {
		if _, err = s.GetChain(ctx, id); err != nil {
			return err
		}
		return store.ErrConflict
	}
	return nil
}

func (s *chains) ListChains(ctx context.Context, page store.Page) ([]store.ChainRecord, error) {
	if err := page.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, "SELECT id,document,revision FROM proxy_chains WHERE id>? ORDER BY id LIMIT ?", string(page.After), page.Limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]store.ChainRecord, 0, page.Limit)
	for rows.Next() {
		var id string
		var document []byte
		var record store.ChainRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Chain) != nil || record.Chain.Validate() != nil || string(record.Chain.ID) != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

var _ store.Chains = (*Store)(nil)
