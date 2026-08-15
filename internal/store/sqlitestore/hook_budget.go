//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks/budget"
)

// SqliteHookBudget implements budget.Dialect over SQLite.
// SQLite lacks RETURNING on UPDATE in all historical versions; we achieve the
// same atomicity via a single transaction: UPDATE WHERE remaining>=cost,
// then SELECT if RowsAffected=1, else UPSERT seed + retry.
type SqliteHookBudget struct {
	db *sql.DB
}

// NewSQLiteHookBudget returns a SqliteHookBudget over db.
func NewSQLiteHookBudget(db *sql.DB) *SqliteHookBudget {
	return &SqliteHookBudget{db: db}
}

// DeductAtomic implements budget.Dialect. See PG equivalent for semantics.
func (b *SqliteHookBudget) DeductAtomic(
	ctx context.Context, tenantID uuid.UUID, cost int64, month time.Time, defaultBudget int64,
) (int64, int64, error) {
	monthStr := month.Format("2006-01-02")

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("budget begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	remaining, total, ok, err := b.tryDeductTx(ctx, tx, tenantID, cost, monthStr)
	if err != nil {
		return 0, 0, err
	}
	if ok {
		if err := tx.Commit(); err != nil {
			return 0, 0, fmt.Errorf("budget commit: %w", err)
		}
		return remaining, total, nil
	}

	if err := b.seedIfStaleTx(ctx, tx, tenantID, monthStr, defaultBudget); err != nil {
		return 0, 0, err
	}

	remaining, total, ok, err = b.tryDeductTx(ctx, tx, tenantID, cost, monthStr)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		_ = tx.Rollback()
		return 0, 0, budget.ErrBudgetExceeded
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("budget commit: %w", err)
	}
	return remaining, total, nil
}

func (b *SqliteHookBudget) tryDeductTx(
	ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, cost int64, monthStr string,
) (int64, int64, bool, error) {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
		UPDATE tenant_hook_budget
		SET remaining = remaining - ?,
		    updated_at = ?
		WHERE tenant_id = ?
		  AND month_start = ?
		  AND remaining >= ?`,
		cost, nowStr, tenantID.String(), monthStr, cost,
	)
	if err != nil {
		return 0, 0, false, fmt.Errorf("budget deduct: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, 0, false, nil
	}

	var remaining, total int64
	err = tx.QueryRowContext(ctx, `
		SELECT remaining, budget_total
		FROM tenant_hook_budget
		WHERE tenant_id = ? AND month_start = ?`,
		tenantID.String(), monthStr,
	).Scan(&remaining, &total)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, false, nil
		}
		return 0, 0, false, fmt.Errorf("budget read-back: %w", err)
	}
	return remaining, total, true, nil
}

func (b *SqliteHookBudget) seedIfStaleTx(
	ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, monthStr string, defaultBudget int64,
) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO tenant_hook_budget
		  (tenant_id, month_start, budget_total, remaining, metadata, updated_at)
		VALUES (?, ?, ?, ?, '{}', ?)
		ON CONFLICT(tenant_id) DO UPDATE
		SET month_start = excluded.month_start,
		    budget_total = excluded.budget_total,
		    remaining = excluded.remaining,
		    updated_at = ?
		WHERE tenant_hook_budget.month_start < excluded.month_start`,
		tenantID.String(), monthStr, defaultBudget, defaultBudget, nowStr, nowStr,
	)
	if err != nil {
		return fmt.Errorf("budget seed: %w", err)
	}
	return nil
}
