package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"time"
)

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM admin_users").Scan(&count); err != nil {
		return 0, safeError(ctx, err)
	}
	return count, nil
}
func (s *Store) CreateUser(ctx context.Context, user auth.User, passwordHash string) error {
	if !user.Validate() || passwordHash == "" {
		return store.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO admin_users(id,username,password_hash,role,enabled,created_at,last_login_at) VALUES (?,?,?,?,?,?,NULL)", string(user.ID), user.Username, passwordHash, string(user.Role), boolInt(user.Enabled), user.CreatedAt.UTC().Unix())
	if err != nil {
		return safeError(ctx, err)
	}
	return nil
}

// A single conditional INSERT prevents two processes from winning first-run setup.
func (s *Store) CreateInitialUser(ctx context.Context, user auth.User, passwordHash string) error {
	if !user.Validate() || user.Role != auth.RoleAdmin || len(passwordHash) < 32 {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO admin_users(id,username,password_hash,role,enabled,created_at,last_login_at)
		SELECT ?,?,?,?,?,?,NULL WHERE NOT EXISTS (SELECT 1 FROM admin_users)`, string(user.ID), user.Username, passwordHash, string(user.Role), boolInt(user.Enabled), user.CreatedAt.UTC().Unix())
	if err != nil {
		return safeError(ctx, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if count != 1 {
		return store.ErrConflict
	}
	return nil
}
func (s *Store) FindUser(ctx context.Context, username string) (auth.User, string, error) {
	var user auth.User
	var passwordHash string
	var enabled int
	var created int64
	var login sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT id,username,password_hash,role,enabled,created_at,last_login_at FROM admin_users WHERE username=?", username).Scan(&user.ID, &user.Username, &passwordHash, &user.Role, &enabled, &created, &login)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, "", store.ErrNotFound
	}
	if err != nil {
		return auth.User{}, "", safeError(ctx, err)
	}
	user.Enabled = enabled == 1
	user.CreatedAt = time.Unix(created, 0).UTC()
	if login.Valid {
		user.LastLoginAt = time.Unix(login.Int64, 0).UTC()
	}
	if !user.Validate() {
		return auth.User{}, "", store.ErrSchema
	}
	return user, passwordHash, nil
}
func (s *Store) UpdateLastLogin(ctx context.Context, id model.ID, at time.Time) error {
	if !id.Valid() {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "UPDATE admin_users SET last_login_at=? WHERE id=?", at.UTC().Unix(), string(id))
	if err != nil {
		return safeError(ctx, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if affected != 1 {
		return store.ErrNotFound
	}
	return nil
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
