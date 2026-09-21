package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/store"
)

// BackupTo creates a consistent SQLite backup at destination. The destination
// must not exist; VACUUM INTO checkpoints WAL state before writing the copy.
func (s *Store) BackupTo(ctx context.Context, destination string) error {
	if destination == "" {
		return store.ErrInvalid
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return store.ErrInvalid
	}
	if info, statErr := os.Stat(abs); statErr == nil {
		if info.IsDir() {
			return store.ErrInvalid
		}
		return store.ErrConflict
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return store.ErrUnavailable
	}
	for _, sidecar := range []string{abs + "-wal", abs + "-shm"} {
		if _, statErr := os.Stat(sidecar); statErr == nil {
			return store.ErrConflict
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return store.ErrUnavailable
		}
	}
	if parent := filepath.Dir(abs); parent == "." {
		return store.ErrInvalid
	} else if info, statErr := os.Stat(parent); statErr != nil || !info.IsDir() {
		return store.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", abs); err != nil {
		return safeError(ctx, err)
	}
	if err := os.Chmod(abs, 0600); err != nil {
		return store.ErrUnavailable
	}
	return nil
}

// Compact reclaims unused pages in an offline database.
func (s *Store) Compact(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return safeError(ctx, err)
	}
	return nil
}

// ValidateBackup checks the embedded migration history without opening the
// backup through Open, which would migrate and mutate an older or empty file.
// A valid older schema is accepted because the normal application open path can
// migrate it after restore; unknown, reordered or tampered histories are not.
func ValidateBackup(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.ErrInvalid
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return store.ErrNotFound
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil || len(names) == 0 {
		return store.ErrSchema
	}

	db, err := openReadOnly(ctx, abs)
	if err != nil {
		return store.ErrSchema
	}
	defer func() { _ = db.Close() }()
	var tableCount int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&tableCount); err != nil || tableCount != 1 {
		return store.ErrSchema
	}
	rows, err := db.QueryContext(ctx, "SELECT version,name,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return store.ErrSchema
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var version int
		var name, checksum string
		if err = rows.Scan(&version, &name, &checksum); err != nil {
			return store.ErrSchema
		}
		if count >= len(names) || version != count+1 || name != names[count] {
			return store.ErrSchema
		}
		migration, readErr := fs.ReadFile(migrations, name)
		if readErr != nil {
			return store.ErrSchema
		}
		digest := sha256.Sum256(migration)
		if checksum != hex.EncodeToString(digest[:]) {
			return store.ErrSchema
		}
		count++
	}
	if err = rows.Err(); err != nil || count == 0 {
		return store.ErrSchema
	}
	if count == len(names) {
		for _, table := range []string{
			"proxy_endpoints", "admin_users", "audit_log", "clients", "api_keys",
			"traffic_events", "traffic_aggregates_minute", "traffic_retention",
			"traffic_dirty_minutes", "traffic_aggregates_hour", "traffic_aggregates_day",
			"traffic_dirty_hours", "traffic_dirty_days", "traffic_aggregate_retention",
			"traffic_cost_aggregates_minute", "traffic_cost_aggregates_hour",
			"traffic_cost_aggregates_day", "budget_usage", "proxy_sources",
			"proxy_pools", "policies", "runtime_snapshots",
		} {
			var tableCount int
			if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableCount); err != nil || tableCount != 1 {
				return store.ErrSchema
			}
		}
	}
	return nil
}

func openReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
