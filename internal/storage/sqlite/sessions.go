package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	publicsession "github.com/nguyenduytan/proxysieve/pkg/session"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const maxDurableSessions = 10_000

func (s *Store) LoadSessions(ctx context.Context) ([]publicsession.Session, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,document FROM sessions ORDER BY json_extract(document,'$.created_at') DESC,id LIMIT ?", maxDurableSessions+1)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]publicsession.Session, 0)
	for rows.Next() {
		var id string
		var document []byte
		var entry publicsession.Session
		if err = rows.Scan(&id, &document); err != nil {
			return nil, safeError(ctx, err)
		}
		if len(document) == 0 || len(document) > 1<<20 || json.Unmarshal(document, &entry) != nil || string(entry.ID) != id || entry.Validate() != nil {
			return nil, store.ErrSchema
		}
		out = append(out, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	if len(out) > maxDurableSessions {
		return nil, store.ErrSchema
	}
	return out, nil
}

func (s *Store) ApplySessions(ctx context.Context, changes publicsession.ChangeSet) error {
	if len(changes.Upserts) == 0 && len(changes.Deletes) == 0 || len(changes.Upserts)+len(changes.Deletes) > maxDurableSessions {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	seen := map[model.ID]bool{}
	for _, entry := range changes.Upserts {
		if entry.Validate() != nil || seen[entry.ID] {
			return store.ErrInvalid
		}
		seen[entry.ID] = true
		document, marshalErr := json.Marshal(entry)
		if marshalErr != nil || len(document) == 0 || len(document) > 1<<20 {
			return store.ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(id,document) VALUES (?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document", string(entry.ID), string(document)); err != nil {
			return safeError(ctx, err)
		}
	}
	for _, id := range changes.Deletes {
		if !id.Valid() || seen[id] {
			return store.ErrInvalid
		}
		seen[id] = true
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE id=?", string(id)); err != nil {
			return safeError(ctx, err)
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		if errors.Is(err, sql.ErrTxDone) {
			return store.ErrUnavailable
		}
		return safeError(ctx, err)
	}
	return nil
}

var _ publicsession.Persistence = (*Store)(nil)
