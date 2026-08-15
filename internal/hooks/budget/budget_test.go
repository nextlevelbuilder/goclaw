package budget_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks/budget"
)

// fakeDialect is an in-memory Dialect for deterministic deduct tests.
// Models tenant_hook_budget row per (tenant_id, month_start).
type fakeDialect struct {
	// row represents the current stored state for the single tenant under test.
	total     int64
	remaining int64
	month     time.Time
	seeded    bool
	// deductCalls counts atomic UPDATEs for race/concurrency assertions.
	deductCalls int
}

func (f *fakeDialect) DeductAtomic(_ context.Context, _ uuid.UUID, cost int64, month time.Time, defaultBudget int64) (int64, int64, error) {
	f.deductCalls++
	if !f.seeded || !f.month.Equal(month) {
		// Month rollover or fresh seed: reset remaining to defaultBudget.
		f.total = defaultBudget
		f.remaining = defaultBudget
		f.month = month
		f.seeded = true
	}
	if f.remaining < cost {
		return 0, 0, budget.ErrBudgetExceeded
	}
	f.remaining -= cost
	return f.remaining, f.total, nil
}

func TestDeduct_Success_ReturnsUpdatedRemaining(t *testing.T) {
	fd := &fakeDialect{}
	s := budget.New(fd, nil)
	got, total, err := s.Deduct(context.Background(), uuid.New(), 1000)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if total != budget.DefaultMonthlyBudget {
		t.Errorf("total=%d, want %d", total, budget.DefaultMonthlyBudget)
	}
	if got != budget.DefaultMonthlyBudget-1000 {
		t.Errorf("remaining=%d, want %d", got, budget.DefaultMonthlyBudget-1000)
	}
}

func TestDeduct_ExceedsBudget_ReturnsErr(t *testing.T) {
	fd := &fakeDialect{}
	s := budget.New(fd, nil)
	tid := uuid.New()
	// Drain most of the budget first.
	if _, _, err := s.Deduct(context.Background(), tid, budget.DefaultMonthlyBudget-500); err != nil {
		t.Fatalf("first deduct err: %v", err)
	}
	// Now request more than remaining — must return ErrBudgetExceeded.
	_, _, err := s.Deduct(context.Background(), tid, 1000)
	if !errors.Is(err, budget.ErrBudgetExceeded) {
		t.Fatalf("want ErrBudgetExceeded, got %v", err)
	}
}

func TestDeduct_MonthRollover_ResetsRemaining(t *testing.T) {
	fd := &fakeDialect{}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	s := budget.New(fd, func() time.Time { return now })

	tid := uuid.New()
	if _, _, err := s.Deduct(context.Background(), tid, 999_999); err != nil {
		t.Fatalf("first deduct: %v", err)
	}

	// Advance to next month; Deduct must seed a fresh row and allow the call.
	now = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	remaining, _, err := s.Deduct(context.Background(), tid, 1)
	if err != nil {
		t.Fatalf("new-month deduct err: %v", err)
	}
	if remaining != budget.DefaultMonthlyBudget-1 {
		t.Errorf("new-month remaining=%d, want %d", remaining, budget.DefaultMonthlyBudget-1)
	}
}

func TestDeduct_ZeroCost_DoesNotDecrement(t *testing.T) {
	fd := &fakeDialect{}
	s := budget.New(fd, nil)
	tid := uuid.New()
	if _, _, err := s.Deduct(context.Background(), tid, 1000); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := fd.remaining
	got, _, err := s.Deduct(context.Background(), tid, 0)
	if err != nil {
		t.Fatalf("zero-cost err: %v", err)
	}
	if got != before {
		t.Errorf("zero-cost mutated remaining: before=%d after=%d", before, got)
	}
}

func TestDeduct_NegativeCost_ReturnsError(t *testing.T) {
	s := budget.New(&fakeDialect{}, nil)
	if _, _, err := s.Deduct(context.Background(), uuid.New(), -1); err == nil {
		t.Fatal("expected error for negative cost")
	}
}

func TestDeduct_NilTenantID_ReturnsError(t *testing.T) {
	s := budget.New(&fakeDialect{}, nil)
	if _, _, err := s.Deduct(context.Background(), uuid.Nil, 100); err == nil {
		t.Fatal("expected error for nil tenant_id")
	}
}

func TestShouldWarn_BelowThreshold_ReturnsTrue(t *testing.T) {
	// 15% remaining is below the 20% warn threshold.
	if !budget.ShouldWarn(150, 1000) {
		t.Error("ShouldWarn(150,1000)=false; want true")
	}
}

func TestShouldWarn_AboveThreshold_ReturnsFalse(t *testing.T) {
	// 50% remaining — well above threshold.
	if budget.ShouldWarn(500, 1000) {
		t.Error("ShouldWarn(500,1000)=true; want false")
	}
}

func TestShouldWarn_ZeroTotal_ReturnsFalse(t *testing.T) {
	if budget.ShouldWarn(0, 0) {
		t.Error("ShouldWarn(0,0) should be false (unlimited budget)")
	}
}
