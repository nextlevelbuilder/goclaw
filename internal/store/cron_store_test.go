package store

import (
	"testing"
	"time"
)

func TestNextRunForToggle_DisableClearsNextRun(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: int64Ptr(60_000),
	}

	if next := NextRunForToggle(schedule, false, now, ""); next != nil {
		t.Fatalf("expected disable toggle to clear next_run_at, got %v", next)
	}
}

func TestNextRunForToggle_EnableRecomputesEverySchedule(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	schedule := &CronSchedule{
		Kind:    "every",
		EveryMS: int64Ptr(60_000),
	}

	next := NextRunForToggle(schedule, true, now, "")
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

	next := NextRunForToggle(schedule, true, now, "America/Toronto")
	if next == nil {
		t.Fatal("expected enable toggle to compute next_run_at for cron schedule")
	}

	want := time.Date(2026, time.March, 28, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("got %v, want %v", next, want)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
