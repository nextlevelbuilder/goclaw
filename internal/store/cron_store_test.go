package store

import (
	"errors"
	"testing"
	"time"
)

func TestNextRunForToggle_DisableClearsNextRun(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: new(int64(60_000)),
	}

	next, err := NextRunForToggle(schedule, false, true, new(now.Add(time.Minute)), now, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next != nil {
		t.Fatalf("expected disable toggle to clear next_run_at, got %v", next)
	}
}

func TestNextRunForToggle_EnableRecomputesEverySchedule(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: new(int64(60_000)),
	}

	next, err := NextRunForToggle(schedule, true, false, nil, now, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == nil {
		t.Fatal("expected enable toggle to recompute next_run_at")
	}

	want := now.Add(time.Minute)
	if !next.Equal(want) {
		t.Fatalf("got %v, want %v", next, want)
	}
}

func TestNextRunForToggle_EnableUsesDefaultTimezoneForCronSchedule(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind: "cron",
		Expr: "0 9 * * *",
	}

	next, err := NextRunForToggle(schedule, true, false, nil, now, "America/Toronto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == nil {
		t.Fatal("expected enable toggle to compute next_run_at for cron schedule")
	}

	want := time.Date(2026, time.March, 28, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("got %v, want %v", next, want)
	}
}

func TestNextRunForToggle_AlreadyEnabledPreservesCurrentNextRun(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	currentNextRun := now.Add(5 * time.Minute)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: new(int64(60_000)),
	}

	next, err := NextRunForToggle(schedule, true, true, &currentNextRun, now.Add(time.Minute), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next == nil {
		t.Fatal("expected preserved next run")
	}
	if !next.Equal(currentNextRun) {
		t.Fatalf("got %v, want %v", next, currentNextRun)
	}
}

func TestNextRunForToggle_ExpiredAtReturnsError(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute).UnixMilli()
	schedule := &CronSchedule{
		Kind: "at",
		AtMS: &past,
	}

	next, err := NextRunForToggle(schedule, true, false, nil, now, "")
	if next != nil {
		t.Fatalf("expected nil next run, got %v", next)
	}
	if err == nil {
		t.Fatal("expected error for expired at schedule")
	}
	if !errors.Is(err, ErrCronJobNoFutureRun) {
		t.Fatalf("got %v, want ErrCronJobNoFutureRun", err)
	}
}

//go:fix inline
func int64Ptr(v int64) *int64 {
	return new(v)
}

//go:fix inline
func timePtr(v time.Time) *time.Time {
	return new(v)
}
