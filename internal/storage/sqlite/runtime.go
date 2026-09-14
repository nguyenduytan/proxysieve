package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

const maxRuntimeDocumentBytes = 16 << 20
const maxRuntimeProxies = 100_000
const maxRuntimePools = 10_000
const maxRuntimePolicies = 1_000
const maxRuntimeHistory = 100

func (s *Store) LoadRuntimeInventory(ctx context.Context) (store.RuntimeBundle, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return store.RuntimeBundle{}, safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	source := struct {
		*endpoints
		*pools
		*policies
	}{&endpoints{q: tx}, &pools{q: tx}, &policies{q: tx}}
	bundle, err := readRuntimeInventory(ctx, source)
	if err != nil {
		return store.RuntimeBundle{}, err
	}
	if err = tx.Commit(); err != nil {
		return store.RuntimeBundle{}, safeError(ctx, err)
	}
	return bundle, nil
}

type runtimeInventoryReader interface {
	store.Endpoints
	store.Pools
	store.Policies
}

func readRuntimeInventory(ctx context.Context, source runtimeInventoryReader) (store.RuntimeBundle, error) {
	var bundle store.RuntimeBundle
	var after model.ID
	for {
		page, err := source.List(ctx, store.Page{After: after, Limit: 1000})
		if err != nil {
			return store.RuntimeBundle{}, err
		}
		for _, record := range page {
			bundle.Proxies = append(bundle.Proxies, record.Endpoint)
			if len(bundle.Proxies) > maxRuntimeProxies {
				return store.RuntimeBundle{}, store.ErrInvalid
			}
		}
		if len(page) < 1000 {
			break
		}
		after = page[len(page)-1].Endpoint.ID
	}
	after = ""
	for {
		page, err := source.ListPools(ctx, store.Page{After: after, Limit: 1000})
		if err != nil {
			return store.RuntimeBundle{}, err
		}
		for _, record := range page {
			bundle.Pools = append(bundle.Pools, record.Pool)
			if len(bundle.Pools) > maxRuntimePools {
				return store.RuntimeBundle{}, store.ErrInvalid
			}
		}
		if len(page) < 1000 {
			break
		}
		after = page[len(page)-1].Pool.ID
	}
	after = ""
	for {
		page, err := source.ListPolicies(ctx, store.Page{After: after, Limit: 1000})
		if err != nil {
			return store.RuntimeBundle{}, err
		}
		for _, record := range page {
			bundle.Policies = append(bundle.Policies, record.Policy)
			if len(bundle.Policies) > maxRuntimePolicies {
				return store.RuntimeBundle{}, store.ErrInvalid
			}
		}
		if len(page) < 1000 {
			break
		}
		after = page[len(page)-1].Policy.ID
	}
	return bundle, nil
}

func (s *Store) CurrentRuntime(ctx context.Context) (store.RuntimeRecord, error) {
	return scanRuntime(ctx, s.db.QueryRowContext(ctx, "SELECT revision,activated_at,activated_by,source_revision,document FROM runtime_snapshots WHERE is_current=1"))
}

func (s *Store) GetRuntime(ctx context.Context, revision int64) (store.RuntimeRecord, error) {
	if revision < 1 {
		return store.RuntimeRecord{}, store.ErrInvalid
	}
	return scanRuntime(ctx, s.db.QueryRowContext(ctx, "SELECT revision,activated_at,activated_by,source_revision,document FROM runtime_snapshots WHERE revision=?", revision))
}

func (s *Store) ListRuntime(ctx context.Context, before int64, limit int) ([]store.RuntimeRecord, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, store.ErrInvalid
	}
	if before == 0 {
		before = math.MaxInt64
	}
	rows, err := s.db.QueryContext(ctx, "SELECT revision,activated_at,activated_by,source_revision,document FROM runtime_snapshots WHERE revision<? ORDER BY revision DESC LIMIT ?", before, limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]store.RuntimeRecord, 0, limit)
	for rows.Next() {
		record, scanErr := scanRuntime(ctx, rows)
		if scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

func (s *Store) ActivateRuntime(ctx context.Context, bundle store.RuntimeBundle, expected int64, actor model.ID, sourceRevision int64, at time.Time) (store.RuntimeRecord, error) {
	if expected < 0 || expected == math.MaxInt64 || !actor.Valid() || sourceRevision < 0 || at.IsZero() {
		return store.RuntimeRecord{}, store.ErrInvalid
	}
	document, err := json.Marshal(bundle)
	if err != nil || len(document) == 0 || len(document) > maxRuntimeDocumentBytes {
		return store.RuntimeRecord{}, store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.RuntimeRecord{}, safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	var current int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM runtime_snapshots WHERE is_current=1").Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
		err = nil
	}
	if err != nil {
		return store.RuntimeRecord{}, safeError(ctx, err)
	}
	if current != expected {
		return store.RuntimeRecord{}, store.ErrConflict
	}
	next := current + 1
	if current > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE runtime_snapshots SET is_current=0 WHERE revision=?", current); err != nil {
			return store.RuntimeRecord{}, safeError(ctx, err)
		}
	}
	stamp := at.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "INSERT INTO runtime_snapshots(revision,activated_at,activated_by,source_revision,document,is_current) VALUES (?,?,?,?,?,1)", next, stamp, string(actor), sourceRevision, document); err != nil {
		return store.RuntimeRecord{}, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM runtime_snapshots WHERE is_current=0 AND revision<=?", next-maxRuntimeHistory); err != nil {
		return store.RuntimeRecord{}, safeError(ctx, err)
	}
	if err = tx.Commit(); err != nil {
		return store.RuntimeRecord{}, safeError(ctx, err)
	}
	return store.RuntimeRecord{Revision: next, Bundle: bundle.Clone(), ActivatedAt: at.UTC(), ActivatedBy: actor, SourceRevision: sourceRevision}, nil
}

type runtimeScanner interface {
	Scan(...any) error
}

func scanRuntime(ctx context.Context, row runtimeScanner) (store.RuntimeRecord, error) {
	var record store.RuntimeRecord
	var activatedAt, activatedBy string
	var document []byte
	if err := row.Scan(&record.Revision, &activatedAt, &activatedBy, &record.SourceRevision, &document); errors.Is(err, sql.ErrNoRows) {
		return store.RuntimeRecord{}, store.ErrNotFound
	} else if err != nil {
		return store.RuntimeRecord{}, safeError(ctx, err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, activatedAt)
	if err != nil || record.Revision < 1 || !model.ID(activatedBy).Valid() || record.SourceRevision < 0 || len(document) == 0 || len(document) > maxRuntimeDocumentBytes || json.Unmarshal(document, &record.Bundle) != nil {
		return store.RuntimeRecord{}, store.ErrSchema
	}
	record.ActivatedAt = stamp.UTC()
	record.ActivatedBy = model.ID(activatedBy)
	record.Bundle = record.Bundle.Clone()
	return record, nil
}

var _ store.RuntimeSnapshots = (*Store)(nil)
