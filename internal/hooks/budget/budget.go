// Package budget implements atomic per-tenant monthly token budgets for the
// prompt hook handler. The single-UPDATE deduct pattern avoids select-then-
// update races (L2 mitigation).
package budget

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrBudgetExceeded is returned when Deduct is called but the remaining
// balance is below the requested cost. Callers should convert to a block
// decision with i18n key "hook.budget_exceeded".
var ErrBudgetExceeded = errors.New("hook budget: insufficient remaining")

// DefaultMonthlyBudget is used when a tenant row is absent: the first call
// of the month seeds a fresh row with this cap. 0 means "unlimited" and
// the deduct path returns remaining=-1 to signal no-cap without a DB write.
const DefaultMonthlyBudget = 1_000_000

// WarnThresholdPct is the percentage of budget_total at which callers should
// warn (log, event). The store does not enforce warn-only behavior; callers
// compare remaining/total against this ratio.
const WarnThresholdPct = 20 // warn when remaining < 20% of total

// Dialect abstracts the per-driver SQL differences (placeholder syntax,
// RETURNING clause support). PG uses $N + RETURNING; SQLite uses ? + a
// separate SELECT after the UPDATE.
type Dialect interface {
	// DeductAtomic performs the atomic deduct + read-back. Returns the new
	// remaining value; returns ErrBudgetExceeded when the row exists but
	// remaining < cost. When the row is missing for the current month, the
	// implementation must UPSERT with budget_total=DefaultMonthlyBudget and
	// deduct from that.
	DeductAtomic(ctx context.Context, tenantID uuid.UUID, cost int64, monthStart time.Time, defaultBudget int64) (remaining int64, total int64, err error)
}

// Store is the public facade used by the prompt handler. It wraps a Dialect
// to apply month-rollover logic and normalize error values.
type Store struct {
	d Dialect
	// Now is injectable for deterministic tests (month rollover assertions).
	Now func() time.Time
}

// New returns a Store that routes writes through d. now may be nil; the
// default time.Now().UTC() is used when unset.
func New(d Dialect, now func() time.Time) *Store {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{d: d, Now: now}
}

// Deduct atomically subtracts cost from the tenant's remaining balance for
// the current month. Returns (remaining, total, nil) on success,
// (_, _, ErrBudgetExceeded) when insufficient, or a wrapped driver error
// otherwise. Seeds a fresh month row with DefaultMonthlyBudget when absent.
//
// cost=0 is always allowed and returns the current remaining (useful for
// pre-flight checks and metrics without a real deduct).
func (s *Store) Deduct(ctx context.Context, tenantID uuid.UUID, cost int64) (remaining int64, total int64, err error) {
	if tenantID == uuid.Nil {
		return 0, 0, fmt.Errorf("budget: nil tenant_id")
	}
	if cost < 0 {
		return 0, 0, fmt.Errorf("budget: negative cost %d", cost)
	}
	month := monthStart(s.Now())
	remaining, total, err = s.d.DeductAtomic(ctx, tenantID, cost, month, DefaultMonthlyBudget)
	if errors.Is(err, sql.ErrNoRows) {
		// Dialect guarantees UPSERT — ErrNoRows here means the row was
		// found but remaining < cost. Normalize to ErrBudgetExceeded so
		// callers don't care about the driver's native signal.
		return 0, 0, ErrBudgetExceeded
	}
	return remaining, total, err
}

// monthStart returns the first day of the month for t in UTC.
func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// ShouldWarn reports true when remaining has crossed below WarnThresholdPct
// of total. Callers decide whether to emit a warn log/event.
func ShouldWarn(remaining, total int64) bool {
	if total <= 0 {
		return false
	}
	pct := remaining * 100 / total
	return pct < WarnThresholdPct
}
