package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type sources struct{ q querier }

func (s *sources) GetSource(ctx context.Context, id model.ID) (store.SourceRecord, error) {
	if !id.Valid() {
		return store.SourceRecord{}, store.ErrInvalid
	}
	var document []byte
	var record store.SourceRecord
	err := s.q.QueryRowContext(ctx, "SELECT document,revision FROM proxy_sources WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Source) != nil || record.Source.Validate() != nil || record.Source.ID != id {
		return store.SourceRecord{}, store.ErrSchema
	}
	return record, nil
}

func (s *sources) PutSource(ctx context.Context, source proxy.Source, expected int64) (store.SourceRecord, error) {
	if source.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.SourceRecord{}, store.ErrInvalid
	}
	document, err := json.Marshal(source)
	if err != nil || len(document) > 1<<20 {
		return store.SourceRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.q.ExecContext(ctx, "INSERT INTO proxy_sources(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(source.ID), string(document))
	} else {
		result, err = s.q.ExecContext(ctx, "UPDATE proxy_sources SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(source.ID), expected)
	}
	if err != nil {
		return store.SourceRecord{}, safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return store.SourceRecord{}, safeError(ctx, err)
	}
	if rows == 0 {
		if expected > 0 {
			if _, err = s.GetSource(ctx, source.ID); err != nil {
				return store.SourceRecord{}, err
			}
		}
		return store.SourceRecord{}, store.ErrConflict
	}
	return store.SourceRecord{Source: source.Clone(), Revision: expected + 1}, nil
}

func (s *sources) DeleteSource(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.q.ExecContext(ctx, "DELETE FROM proxy_sources WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if rows == 0 {
		if _, err = s.GetSource(ctx, id); err != nil {
			return err
		}
		return store.ErrConflict
	}
	return nil
}

func (s *sources) ListSources(ctx context.Context, page store.Page) ([]store.SourceRecord, error) {
	if err := page.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, "SELECT id,document,revision FROM proxy_sources WHERE id>? ORDER BY id LIMIT ?", string(page.After), page.Limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]store.SourceRecord, 0, page.Limit)
	for rows.Next() {
		var id string
		var document []byte
		var record store.SourceRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Source) != nil || record.Source.Validate() != nil || string(record.Source.ID) != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}
