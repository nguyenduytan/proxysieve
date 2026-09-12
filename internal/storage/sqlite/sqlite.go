// Package sqlite is the SQLite adapter; driver types never escape its boundary.
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/store"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
	endpoints
	sources
}

// Open accepts a filesystem path, never caller-controlled SQLite URI parameters.
// The parent directory must already exist. No secrets are stored in these tables.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || strings.HasPrefix(path, "file:") || path == ":memory:" {
		return nil, store.ErrInvalid
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, store.ErrInvalid
	}
	info, err := os.Lstat(abs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrUnavailable
	}
	if err == nil && !info.Mode().IsRegular() {
		return nil, store.ErrInvalid
	}
	if errors.Is(err, os.ErrNotExist) {
		f, e := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil && !errors.Is(e, os.ErrExist) {
			return nil, store.ErrUnavailable
		}
		if e == nil {
			if f.Close() != nil {
				return nil, store.ErrUnavailable
			}
		}
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, store.ErrUnavailable
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, endpoints: endpoints{q: db}, sources: sources{q: db}}
	if err = db.PingContext(ctx); err == nil {
		_, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL")
	}
	if err == nil {
		err = s.migrate(ctx, migrations)
	}
	if err != nil {
		_ = db.Close()
		return nil, safeError(ctx, err)
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) WithinTransaction(ctx context.Context, fn func(store.Endpoints) error) error {
	if fn == nil {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(&endpoints{q: tx}); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return safeError(ctx, tx.Commit())
}

func (s *Store) WithinInventoryTransaction(ctx context.Context, fn func(store.Endpoints, store.Sources) error) error {
	if fn == nil {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(&endpoints{q: tx}, &sources{q: tx}); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return safeError(ctx, tx.Commit())
}

type Status struct {
	SchemaVersion int    `json:"schema_version"`
	EndpointCount int    `json:"endpoint_count"`
	SourceCount   int    `json:"source_count"`
	JournalMode   string `json:"journal_mode"`
}

func (s *Store) Status(ctx context.Context) (Status, error) {
	var status Status
	if err := s.db.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM schema_migrations").Scan(&status.SchemaVersion); err != nil {
		return Status{}, safeError(ctx, err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM proxy_endpoints").Scan(&status.EndpointCount); err != nil {
		return Status{}, safeError(ctx, err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM proxy_sources").Scan(&status.SourceCount); err != nil {
		return Status{}, safeError(ctx, err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&status.JournalMode); err != nil {
		return Status{}, safeError(ctx, err)
	}
	return status, nil
}

func (s *Store) migrate(ctx context.Context, files fs.FS) error {
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil || len(names) == 0 {
		return store.ErrSchema
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
	if _, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL) STRICT`); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, "SELECT version,name,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var version int
		var name, sum string
		if err = rows.Scan(&version, &name, &sum); err != nil {
			break
		}
		count++
		if version != count || count > len(names) || name != names[count-1] {
			err = store.ErrSchema
			break
		}
		b, e := fs.ReadFile(files, name)
		if e != nil {
			err = store.ErrSchema
			break
		}
		digest := sha256.Sum256(b)
		if hex.EncodeToString(digest[:]) != sum {
			err = store.ErrSchema
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	for i := count; i < len(names); i++ {
		b, e := fs.ReadFile(files, names[i])
		if e != nil {
			return store.ErrSchema
		}
		if _, err = conn.ExecContext(ctx, string(b)); err != nil {
			return err
		}
		digest := sha256.Sum256(b)
		if _, err = conn.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,checksum) VALUES (?,?,?)", i+1, names[i], hex.EncodeToString(digest[:])); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
func safeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, store.ErrSchema) {
		return store.ErrSchema
	}
	return store.ErrUnavailable // Raw driver errors can include paths or SQL contents.
}

var _ store.EndpointStore = (*Store)(nil)
var _ store.Sources = (*Store)(nil)
var _ store.InventoryStore = (*Store)(nil)
