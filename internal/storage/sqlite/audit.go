package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/audit"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func (s *Store) Record(ctx context.Context, event audit.Event) error {
	if !event.Validate() {
		return audit.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO audit_log(id,at,actor_id,action,target_type,target_id,request_id) VALUES (?,?,?,?,?,?,?)", string(event.ID), event.At.UTC().UnixNano(), nullableID(event.ActorID), event.Action, event.TargetType, nullableString(event.TargetID), nullableID(event.RequestID))
	if err != nil {
		return safeError(ctx, err)
	}
	return nil
}
func (s *Store) ListAudit(ctx context.Context, page audit.Page) ([]audit.Event, error) {
	if !page.Valid() {
		return nil, audit.ErrInvalid
	}
	args := []any{}
	query := "SELECT id,at,actor_id,action,target_type,target_id,request_id FROM audit_log"
	if !page.Before.IsZero() {
		if page.BeforeID == "" {
			query += " WHERE at < ?"
			args = append(args, page.Before.UTC().UnixNano())
		} else {
			query += " WHERE (at < ? OR (at = ? AND id < ?))"
			before := page.Before.UTC().UnixNano()
			args = append(args, before, before, string(page.BeforeID))
		}
	}
	query += " ORDER BY at DESC,id DESC LIMIT ?"
	args = append(args, page.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]audit.Event, 0, page.Limit)
	for rows.Next() {
		var event audit.Event
		var at int64
		var actor, target, request sql.NullString
		if err = rows.Scan(&event.ID, &at, &actor, &event.Action, &event.TargetType, &target, &request); err != nil {
			return nil, safeError(ctx, err)
		}
		event.At = time.Unix(0, at).UTC()
		if actor.Valid {
			event.ActorID = model.ID(actor.String)
		}
		if target.Valid {
			event.TargetID = target.String
		}
		if request.Valid {
			event.RequestID = model.ID(request.String)
		}
		if !event.Validate() {
			return nil, store.ErrSchema
		}
		out = append(out, event)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return out, nil
}
func nullableID(id model.ID) any {
	if id == "" {
		return nil
	}
	return string(id)
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ audit.Writer = (*Store)(nil)
var _ audit.Reader = (*Store)(nil)
