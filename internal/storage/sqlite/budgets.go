package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

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
		var used, reserved int64
		if err = tx.QueryRowContext(ctx, "SELECT used_bytes,reserved_bytes FROM budget_usage WHERE budget_id=? AND window_start=?", string(configured.ID), windowStart).Scan(&used, &reserved); err != nil {
			return safeError(ctx, err)
		}
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
	windowStart, err := budgetWindowStart(configured, at)
	if err != nil {
		return publicbudget.Usage{}, err
	}
	var used, reserved int64
	err = s.db.QueryRowContext(ctx, "SELECT used_bytes,reserved_bytes FROM budget_usage WHERE budget_id=? AND window_start=?", string(configured.ID), windowStart).Scan(&used, &reserved)
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
	return start.UnixNano(), nil
}
