package store

import (
	"context"
	"encoding/json"
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

func TestCronPayloadCredentialUserIDJSONRoundTrip(t *testing.T) {
	payload := CronPayload{
		Kind:             "agent_turn",
		Message:          "run credentialed report",
		CredentialUserID: "tenant-user-123",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("payload JSON is invalid: %s", data)
	}

	var got CronPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.CredentialUserID != payload.CredentialUserID {
		t.Fatalf("credential user ID = %q, want %q", got.CredentialUserID, payload.CredentialUserID)
	}
}

func TestCronPayloadLegacyJSONKeepsEmptyCredentialUserID(t *testing.T) {
	var got CronPayload
	if err := json.Unmarshal([]byte(`{"kind":"agent_turn","message":"legacy"}`), &got); err != nil {
		t.Fatalf("unmarshal legacy payload: %v", err)
	}
	if got.CredentialUserID != "" {
		t.Fatalf("legacy payload credential user ID = %q, want empty", got.CredentialUserID)
	}
}

func TestCheckCronCredentialOwnerBlocksMismatch(t *testing.T) {
	ctx := WithCredentialUserID(context.Background(), "tenant-user-b")
	job := &CronJob{Payload: CronPayload{CredentialUserID: "tenant-user-a"}}

	if err := CheckCronCredentialOwner(ctx, job); !errors.Is(err, ErrCronCredentialOwnerMismatch) {
		t.Fatalf("got %v, want ErrCronCredentialOwnerMismatch", err)
	}
}

func TestCheckCronCredentialOwnerIgnoresUserIDFallback(t *testing.T) {
	ctx := WithUserID(context.Background(), "tenant-user-a")
	job := &CronJob{Payload: CronPayload{CredentialUserID: "tenant-user-a"}}

	if err := CheckCronCredentialOwner(ctx, job); !errors.Is(err, ErrCronCredentialOwnerMismatch) {
		t.Fatalf("got %v, want ErrCronCredentialOwnerMismatch", err)
	}
}

func TestCheckCronCredentialOwnerAllowsMatchAndLegacy(t *testing.T) {
	ctx := WithCredentialUserID(context.Background(), "tenant-user-a")
	if err := CheckCronCredentialOwner(ctx, &CronJob{Payload: CronPayload{CredentialUserID: "tenant-user-a"}}); err != nil {
		t.Fatalf("matching credential owner should pass: %v", err)
	}
	if err := CheckCronCredentialOwner(context.Background(), &CronJob{}); err != nil {
		t.Fatalf("legacy job should pass: %v", err)
	}
}

func TestRedactCronJobCredentialContext(t *testing.T) {
	job := CronJob{Payload: CronPayload{CredentialUserID: "tenant-user-a", Message: "run"}}
	redacted := RedactCronJobCredentialContext(job)

	if redacted.Payload.CredentialUserID != "" {
		t.Fatalf("redacted credential user ID = %q, want empty", redacted.Payload.CredentialUserID)
	}
	if job.Payload.CredentialUserID == "" {
		t.Fatal("redaction mutated source job")
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
