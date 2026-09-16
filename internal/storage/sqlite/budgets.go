package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

func (s *Store) InitializeBudgets(ctx context.Context, configured []publicbudget.Config) ([]store.BudgetRecord, error) {
	documents := make(map[model.ID][]byte, len(configured))
	for _, item := range configured {
		document, err := marshalBudget(item)
		if err != nil || documents[item.ID] != nil {
			return nil, store.ErrInvalid
		}
		documents[item.ID] = document
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	claim, err := tx.ExecContext(ctx, "UPDATE budget_inventory_state SET initialized=1 WHERE singleton=1 AND initialized=0")
	if err != nil {
		return nil, safeError(ctx, err)
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return nil, safeError(ctx, err)
	}
	if claimed == 1 {
		ids := make([]model.ID, 0, len(documents))
		for id := range documents {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if _, err = tx.ExecContext(ctx, "INSERT INTO budget_configs(id,revision,document) VALUES (?,1,?)", string(id), string(documents[id])); err != nil {
				return nil, safeError(ctx, err)
			}
		}
	} else {
		var initialized bool
		if claimed != 0 {
			return nil, store.ErrSchema
		}
		if err = tx.QueryRowContext(ctx, "SELECT initialized FROM budget_inventory_state WHERE singleton=1").Scan(&initialized); err != nil {
			return nil, safeError(ctx, err)
		}
		if !initialized {
			return nil, store.ErrSchema
		}
	}
	records, err := listBudgets(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

func (s *Store) GetBudget(ctx context.Context, id model.ID) (store.BudgetRecord, error) {
	return getBudget(ctx, s.db, id)
}

func (s *Store) PutBudget(ctx context.Context, configured publicbudget.Config, expected int64) (store.BudgetRecord, error) {
	document, err := marshalBudget(configured)
	if err != nil || expected < 0 || expected == math.MaxInt64 {
		return store.BudgetRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO budget_configs(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(configured.ID), string(document))
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE budget_configs SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(document), string(configured.ID), expected)
	}
	if err != nil {
		return store.BudgetRecord{}, safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return store.BudgetRecord{}, safeError(ctx, err)
	}
	if changed == 0 {
		if expected > 0 {
			if _, err = s.GetBudget(ctx, configured.ID); err != nil {
				return store.BudgetRecord{}, err
			}
		}
		return store.BudgetRecord{}, store.ErrConflict
	}
	return store.BudgetRecord{Budget: configured, Revision: expected + 1}, nil
}

func (s *Store) DeleteBudget(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM budget_configs WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if changed == 1 {
		return nil
	}
	if _, err = s.GetBudget(ctx, id); err != nil {
		return err
	}
	return store.ErrConflict
}

func (s *Store) ListBudgets(ctx context.Context) ([]store.BudgetRecord, error) {
	return listBudgets(ctx, s.db)
}

func marshalBudget(configured publicbudget.Config) ([]byte, error) {
	if configured.Validate() != nil {
		return nil, store.ErrInvalid
	}
	document, err := json.Marshal(configured)
	if err != nil || len(document) > 64<<10 {
		return nil, store.ErrInvalid
	}
	return document, nil
}

func getBudget(ctx context.Context, q querier, id model.ID) (store.BudgetRecord, error) {
	if !id.Valid() {
		return store.BudgetRecord{}, store.ErrInvalid
	}
	var document []byte
	var record store.BudgetRecord
	err := q.QueryRowContext(ctx, "SELECT document,revision FROM budget_configs WHERE id=?", string(id)).Scan(&document, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record, store.ErrNotFound
	}
	if err != nil {
		return record, safeError(ctx, err)
	}
	if json.Unmarshal(document, &record.Budget) != nil || record.Budget.Validate() != nil || record.Budget.ID != id {
		return store.BudgetRecord{}, store.ErrSchema
	}
	return record, nil
}

func listBudgets(ctx context.Context, q querier) ([]store.BudgetRecord, error) {
	rows, err := q.QueryContext(ctx, "SELECT id,document,revision FROM budget_configs ORDER BY id")
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := []store.BudgetRecord{}
	for rows.Next() {
		var id model.ID
		var document []byte
		var record store.BudgetRecord
		if err = rows.Scan(&id, &document, &record.Revision); err != nil {
			return nil, safeError(ctx, err)
		}
		if json.Unmarshal(document, &record.Budget) != nil || record.Budget.Validate() != nil || record.Budget.ID != id {
			return nil, store.ErrSchema
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

func (s *Store) ReserveBudgets(ctx context.Context, configs []publicbudget.Config, at time.Time, amount traffic.Bytes) error {
	if len(configs) == 0 || amount == 0 || uint64(amount) > math.MaxInt64 {
		return publicbudget.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, configured := range configs {
		if configured.Validate() != nil || uint64(configured.Limit) > math.MaxInt64 {
			return publicbudget.ErrInvalid
		}
		windowStart, windowErr := budgetWindowStart(configured, at)
		if windowErr != nil {
			return windowErr
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO budget_usage(budget_id,window_start) VALUES (?,?)", string(configured.ID), windowStart); err != nil {
			return safeError(ctx, err)
		}
		usage, usageErr := readBudgetUsage(ctx, tx, configured, at)
		if usageErr != nil {
			return usageErr
		}
		used, reserved := int64(usage.Used), int64(usage.Reserved)
		requested := int64(amount)
		limit := int64(configured.Limit)
		if used > math.MaxInt64-reserved || reserved > math.MaxInt64-requested || configured.Hard && (used+reserved > limit || requested > limit-used-reserved) {
			return publicbudget.ErrExceeded
		}
	}
	for _, configured := range configs {
		windowStart, _ := budgetWindowStart(configured, at)
		if _, err = tx.ExecContext(ctx, "UPDATE budget_usage SET reserved_bytes=reserved_bytes+? WHERE budget_id=? AND window_start=?", int64(amount), string(configured.ID), windowStart); err != nil {
			return safeError(ctx, err)
		}
	}
	return safeError(ctx, tx.Commit())
}

func (s *Store) ConsumeBudgets(ctx context.Context, configs []publicbudget.Config, at time.Time, amount traffic.Bytes) error {
	return s.moveBudgetReservation(ctx, configs, at, amount, true)
}

func (s *Store) ReleaseBudgets(ctx context.Context, configs []publicbudget.Config, at time.Time, amount traffic.Bytes) error {
	return s.moveBudgetReservation(ctx, configs, at, amount, false)
}

func (s *Store) moveBudgetReservation(ctx context.Context, configs []publicbudget.Config, at time.Time, amount traffic.Bytes, consume bool) error {
	if len(configs) == 0 || uint64(amount) > math.MaxInt64 {
		return publicbudget.ErrInvalid
	}
	if amount == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, configured := range configs {
		windowStart, windowErr := budgetWindowStart(configured, at)
		if windowErr != nil {
			return windowErr
		}
		statement := "UPDATE budget_usage SET reserved_bytes=reserved_bytes-? WHERE budget_id=? AND window_start=? AND reserved_bytes>=?"
		if consume {
			statement = "UPDATE budget_usage SET reserved_bytes=reserved_bytes-?,used_bytes=used_bytes+? WHERE budget_id=? AND window_start=? AND reserved_bytes>=? AND used_bytes<=?"
		}
		var result sql.Result
		if consume {
			result, err = tx.ExecContext(ctx, statement, int64(amount), int64(amount), string(configured.ID), windowStart, int64(amount), math.MaxInt64-int64(amount))
		} else {
			result, err = tx.ExecContext(ctx, statement, int64(amount), string(configured.ID), windowStart, int64(amount))
		}
		if err != nil {
			return safeError(ctx, err)
		}
		if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
			if changeErr != nil {
				return safeError(ctx, changeErr)
			}
			return store.ErrSchema
		}
	}
	return safeError(ctx, tx.Commit())
}

func (s *Store) BudgetUsage(ctx context.Context, configured publicbudget.Config, at time.Time) (publicbudget.Usage, error) {
	return readBudgetUsage(ctx, s.db, configured, at)
}

func readBudgetUsage(ctx context.Context, q querier, configured publicbudget.Config, at time.Time) (publicbudget.Usage, error) {
	windowStart, err := budgetWindowStart(configured, at)
	if err != nil {
		return publicbudget.Usage{}, err
	}
	var used, reserved int64
	query := "SELECT used_bytes,reserved_bytes FROM budget_usage WHERE budget_id=? AND window_start=?"
	args := []any{string(configured.ID), windowStart}
	if configured.Window == publicbudget.WindowRolling {
		start, _, boundsErr := configured.WindowBounds(at)
		if boundsErr != nil {
			return publicbudget.Usage{}, boundsErr
		}
		query = "SELECT COALESCE(SUM(used_bytes),0),COALESCE(SUM(reserved_bytes),0) FROM budget_usage WHERE budget_id=? AND window_start>=?"
		args = []any{string(configured.ID), start.UTC().Truncate(time.Minute).UnixNano()}
	}
	err = q.QueryRowContext(ctx, query, args...).Scan(&used, &reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return publicbudget.Usage{}, nil
	}
	if err != nil {
		return publicbudget.Usage{}, safeError(ctx, err)
	}
	if used < 0 || reserved < 0 {
		return publicbudget.Usage{}, store.ErrSchema
	}
	return publicbudget.Usage{Used: traffic.Bytes(used), Reserved: traffic.Bytes(reserved)}, nil
}

// RecoverBudgetReservations conservatively charges every reservation left by a
// crashed process. This can over-count at most the configured in-flight chunk per
// lease, but it never makes a restart reset hard-budget enforcement.
func (s *Store) RecoverBudgetReservations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE budget_usage
SET used_bytes=used_bytes+reserved_bytes,reserved_bytes=0
WHERE reserved_bytes>0 AND used_bytes<=9223372036854775807-reserved_bytes`)
	if err != nil {
		return safeError(ctx, err)
	}
	var unsafe int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM budget_usage WHERE reserved_bytes>0").Scan(&unsafe); err != nil {
		return safeError(ctx, err)
	}
	if unsafe != 0 {
		return store.ErrSchema
	}
	return nil
}

func budgetWindowStart(configured publicbudget.Config, at time.Time) (int64, error) {
	start, _, err := configured.WindowBounds(at)
	if err != nil {
		return 0, err
	}
	if start.IsZero() {
		return 0, nil
	}
	if configured.Window == publicbudget.WindowRolling {
		return at.UTC().Truncate(time.Minute).UnixNano(), nil
	}
	return start.UnixNano(), nil
}
