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
	if !user.Validate() || len(passwordHash) < 32 || len(passwordHash) > 1024 {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "INSERT INTO admin_users(id,username,password_hash,role,enabled,created_at,last_login_at) VALUES (?,?,?,?,?,?,NULL) ON CONFLICT DO NOTHING", string(user.ID), user.Username, passwordHash, string(user.Role), boolInt(user.Enabled), user.CreatedAt.UTC().Unix())
	if err != nil {
		return safeError(ctx, err)
	}
	if changed, changeErr := result.RowsAffected(); changeErr != nil {
		return safeError(ctx, changeErr)
	} else if changed != 1 {
		return store.ErrConflict
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context) ([]auth.User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,username,role,enabled,created_at,last_login_at FROM admin_users ORDER BY username COLLATE NOCASE,id")
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	users := []auth.User{}
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return users, nil
}

func (s *Store) GetUser(ctx context.Context, id model.ID) (auth.User, error) {
	if !id.Valid() {
		return auth.User{}, store.ErrInvalid
	}
	return scanUser(s.db.QueryRowContext(ctx, "SELECT id,username,role,enabled,created_at,last_login_at FROM admin_users WHERE id=?", string(id)))
}

func (s *Store) UpdateUser(ctx context.Context, user auth.User, passwordHash string) error {
	if !user.Validate() || len(passwordHash) > 1024 {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := scanUser(tx.QueryRowContext(ctx, "SELECT id,username,role,enabled,created_at,last_login_at FROM admin_users WHERE id=?", string(user.ID)))
	if err != nil {
		return err
	}
	if current.Role == auth.RoleAdmin && current.Enabled && (user.Role != auth.RoleAdmin || !user.Enabled) {
		var others int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_users WHERE id<>? AND role='admin' AND enabled=1", string(user.ID)).Scan(&others); err != nil {
			return safeError(ctx, err)
		}
		if others == 0 {
			return store.ErrConflict
		}
	}
	var duplicate int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_users WHERE id<>? AND username=? COLLATE NOCASE", string(user.ID), user.Username).Scan(&duplicate); err != nil {
		return safeError(ctx, err)
	}
	if duplicate != 0 {
		return store.ErrConflict
	}
	var result sql.Result
	if passwordHash == "" {
		result, err = tx.ExecContext(ctx, "UPDATE admin_users SET username=?,role=?,enabled=? WHERE id=?", user.Username, string(user.Role), boolInt(user.Enabled), string(user.ID))
	} else if len(passwordHash) < 32 {
		return store.ErrInvalid
	} else {
		result, err = tx.ExecContext(ctx, "UPDATE admin_users SET username=?,password_hash=?,role=?,enabled=? WHERE id=?", user.Username, passwordHash, string(user.Role), boolInt(user.Enabled), string(user.ID))
	}
	if err != nil {
		return safeError(ctx, err)
	}
	if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
		if changeErr != nil {
			return safeError(ctx, changeErr)
		}
		return store.ErrNotFound
	}
	return safeError(ctx, tx.Commit())
}

func (s *Store) DeleteUser(ctx context.Context, id model.ID) error {
	if !id.Valid() {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := scanUser(tx.QueryRowContext(ctx, "SELECT id,username,role,enabled,created_at,last_login_at FROM admin_users WHERE id=?", string(id)))
	if err != nil {
		return err
	}
	if current.Role == auth.RoleAdmin && current.Enabled {
		var others int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_users WHERE id<>? AND role='admin' AND enabled=1", string(id)).Scan(&others); err != nil {
			return safeError(ctx, err)
		}
		if others == 0 {
			return store.ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM admin_users WHERE id=?", string(id)); err != nil {
		return safeError(ctx, err)
	}
	return safeError(ctx, tx.Commit())
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

type userScanner interface{ Scan(...any) error }

func scanUser(row userScanner) (auth.User, error) {
	var user auth.User
	var enabled int
	var created int64
	var login sql.NullInt64
	err := row.Scan(&user.ID, &user.Username, &user.Role, &enabled, &created, &login)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, store.ErrNotFound
	}
	if err != nil {
		return auth.User{}, store.ErrUnavailable
	}
	user.Enabled = enabled == 1
	user.CreatedAt = time.Unix(created, 0).UTC()
	if login.Valid {
		user.LastLoginAt = time.Unix(login.Int64, 0).UTC()
	}
	if !user.Validate() {
		return auth.User{}, store.ErrSchema
	}
	return user, nil
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
