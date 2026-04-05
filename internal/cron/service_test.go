package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// --- Schedule validation ---

func TestValidateSchedule(t *testing.T) {
	cs := NewService("", nil)

	tests := []struct {
		name    string
		sched   Schedule
		wantErr bool
	}{
		{"at_valid", Schedule{Kind: "at", AtMS: new(time.Now().Add(time.Hour).UnixMilli())}, false},
		{"at_missing_timestamp", Schedule{Kind: "at"}, true},
		{"every_valid", Schedule{Kind: "every", EveryMS: new(int64(5000))}, false},
		{"every_zero_interval", Schedule{Kind: "every", EveryMS: new(int64(0))}, true},
		{"every_negative_interval", Schedule{Kind: "every", EveryMS: new(int64(-1))}, true},
		{"every_nil_interval", Schedule{Kind: "every"}, true},
		{"cron_valid", Schedule{Kind: "cron", Expr: "*/5 * * * *"}, false},
		{"cron_empty_expr", Schedule{Kind: "cron", Expr: ""}, true},
		{"cron_invalid_expr", Schedule{Kind: "cron", Expr: "bad cron"}, true},
		{"cron_valid_with_tz", Schedule{Kind: "cron", Expr: "0 9 * * *", TZ: "Asia/Saigon"}, false},
		{"cron_invalid_tz", Schedule{Kind: "cron", Expr: "0 9 * * *", TZ: "Invalid/Zone"}, true},
		{"unknown_kind", Schedule{Kind: "invalid"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cs.validateSchedule(&tt.sched)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSchedule() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// --- computeNextRun ---

func TestComputeNextRun(t *testing.T) {
	cs := NewService("", nil)
	now := time.Now().UnixMilli()

	t.Run("at_future", func(t *testing.T) {
		future := now + 60000
		sched := Schedule{Kind: "at", AtMS: &future}
		next := cs.computeNextRun(&sched, now)
		if next == nil || *next != future {
			t.Fatalf("expected %d, got %v", future, next)
		}
	})

	t.Run("at_past", func(t *testing.T) {
		past := now - 60000
		sched := Schedule{Kind: "at", AtMS: &past}
		next := cs.computeNextRun(&sched, now)
		if next != nil {
			t.Fatalf("past at-schedule should return nil, got %d", *next)
		}
	})

	t.Run("every_5s", func(t *testing.T) {
		interval := int64(5000)
		sched := Schedule{Kind: "every", EveryMS: &interval}
		next := cs.computeNextRun(&sched, now)
		if next == nil {
			t.Fatal("expected non-nil next")
		}
		expected := now + 5000
		if *next != expected {
			t.Fatalf("expected %d, got %d", expected, *next)
		}
	})

	t.Run("every_nil_interval", func(t *testing.T) {
		sched := Schedule{Kind: "every"}
		next := cs.computeNextRun(&sched, now)
		if next != nil {
			t.Fatal("nil interval should return nil")
		}
	})

	t.Run("cron_every_minute", func(t *testing.T) {
		sched := Schedule{Kind: "cron", Expr: "* * * * *"}
		next := cs.computeNextRun(&sched, now)
		if next == nil {
			t.Fatal("expected non-nil next for every-minute cron")
		}
		// Should be within next 60 seconds
		diff := *next - now
		if diff < 0 || diff > 61000 {
			t.Fatalf("next run should be within 61s, got diff=%dms", diff)
		}
	})

	t.Run("cron_empty_expr", func(t *testing.T) {
		sched := Schedule{Kind: "cron", Expr: ""}
		next := cs.computeNextRun(&sched, now)
		if next != nil {
			t.Fatal("empty expr should return nil")
		}
	})

	t.Run("unknown_kind", func(t *testing.T) {
		sched := Schedule{Kind: "wut"}
		next := cs.computeNextRun(&sched, now)
		if next != nil {
			t.Fatal("unknown kind should return nil")
		}
	})
}

// --- AddJob / RemoveJob / ListJobs / EnableJob ---

func TestService_CRUD(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "cron.json")
	cs := NewService(storePath, nil)

	// Add a job
	interval := int64(60000)
	job, err := cs.AddJob("test-job", Schedule{Kind: "every", EveryMS: &interval}, "hello", false, "", "", "agent-1")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if job.ID == "" {
		t.Fatal("job ID should not be empty")
	}
	if job.Name != "test-job" {
		t.Fatalf("job name: got %q", job.Name)
	}
	if !job.Enabled {
		t.Fatal("new job should be enabled")
	}
	if job.State.NextRunAtMS == nil {
		t.Fatal("new job should have NextRunAtMS set")
	}

	// List jobs
	jobs := cs.ListJobs(false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	// Disable job
	if err := cs.EnableJob(job.ID, false); err != nil {
		t.Fatalf("EnableJob error: %v", err)
	}
	jobs = cs.ListJobs(false) // excludes disabled
	if len(jobs) != 0 {
		t.Fatalf("expected 0 enabled jobs, got %d", len(jobs))
	}
	jobs = cs.ListJobs(true) // includes disabled
	if len(jobs) != 1 {
		t.Fatalf("expected 1 total job, got %d", len(jobs))
	}

	// Re-enable
	if err := cs.EnableJob(job.ID, true); err != nil {
		t.Fatalf("EnableJob error: %v", err)
	}

	// Remove
	if err := cs.RemoveJob(job.ID); err != nil {
		t.Fatalf("RemoveJob error: %v", err)
	}
	jobs = cs.ListJobs(true)
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs after remove, got %d", len(jobs))
	}

	// Verify persisted
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Fatal("store file should exist")
	}
}

func TestService_AddJob_InvalidSchedule(t *testing.T) {
	cs := NewService("", nil)
	_, err := cs.AddJob("bad", Schedule{Kind: "unknown"}, "msg", false, "", "", "")
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

func TestService_RemoveJob_NotFound(t *testing.T) {
	cs := NewService("", nil)
	err := cs.RemoveJob("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

func TestService_EnableJob_NotFound(t *testing.T) {
	cs := NewService("", nil)
	err := cs.EnableJob("nonexistent", true)
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

// --- At-schedule sets DeleteAfterRun ---

func TestService_AddJob_AtSchedule_DeleteAfterRun(t *testing.T) {
	cs := NewService("", nil)
	future := time.Now().Add(time.Hour).UnixMilli()
	job, err := cs.AddJob("one-shot", Schedule{Kind: "at", AtMS: &future}, "run once", false, "", "", "")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}
	if !job.DeleteAfterRun {
		t.Fatal("at-schedule should set DeleteAfterRun=true")
	}
}

// --- Job execution callback ---

func TestService_StartStop_JobExecution(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "cron.json")

	var execCount atomic.Int32
	handler := func(job *Job) (string, error) {
		execCount.Add(1)
		return "done", nil
	}

	cs := NewService(storePath, handler)

	// Add a fast-interval job (every 100ms) — but runLoop ticks every 1s
	interval := int64(100)
	_, err := cs.AddJob("fast", Schedule{Kind: "every", EveryMS: &interval}, "tick", false, "", "", "")
	if err != nil {
		t.Fatalf("AddJob error: %v", err)
	}

	if err := cs.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// runLoop ticks every 1s, wait enough for at least 1 tick
	time.Sleep(1500 * time.Millisecond)
	cs.Stop()

	count := execCount.Load()
	if count == 0 {
		t.Fatal("expected at least 1 job execution")
	}
}

// --- Handler not set → no panic ---

func TestService_NilHandler_NoPanic(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "cron.json")

	cs := NewService(storePath, nil) // no handler

	interval := int64(100)
	cs.AddJob("no-handler", Schedule{Kind: "every", EveryMS: &interval}, "tick", false, "", "", "")

	cs.Start()
	time.Sleep(1500 * time.Millisecond) // wait for at least 1 tick
	cs.Stop()                           // should not panic
}

// --- Job failure with retry ---

func TestService_JobFailure_Updates_LastError(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "cron.json")

	handler := func(job *Job) (string, error) {
		return "", fmt.Errorf("intentional failure")
	}

	cs := NewService(storePath, handler)
	cs.SetRetryConfig(RetryConfig{MaxRetries: 0}) // no retry

	interval := int64(100)
	job, _ := cs.AddJob("failing", Schedule{Kind: "every", EveryMS: &interval}, "fail", false, "", "", "")

	cs.Start()
	time.Sleep(1500 * time.Millisecond) // wait for at least 1 tick
	cs.Stop()

	// Check last error
	found, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatal("job should exist")
	}
	if found.State.LastStatus != "error" {
		t.Fatalf("expected last status 'error', got %q", found.State.LastStatus)
	}
	if found.State.LastError == "" {
		t.Fatal("expected non-empty last error")
	}
}

// --- Persistence: save and reload ---

func TestService_Persistence_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "cron.json")

	cs1 := NewService(storePath, nil)
	interval := int64(60000)
	cs1.AddJob("persist-test", Schedule{Kind: "every", EveryMS: &interval}, "msg", false, "", "", "agent-1")

	// New service should load the persisted job
	cs2 := NewService(storePath, nil)
	cs2.Start()
	defer cs2.Stop()

	jobs := cs2.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(jobs))
	}
	if jobs[0].Name != "persist-test" {
		t.Fatalf("job name mismatch: got %q", jobs[0].Name)
	}
}

// --- Run log ---

func TestService_RunLog_PopulatedByAutoExecution(t *testing.T) {
	dir := t.TempDir()
	cs := NewService(filepath.Join(dir, "cron.json"), func(job *Job) (string, error) {
		return "ok", nil
	})

	interval := int64(100)
	job, _ := cs.AddJob("logger", Schedule{Kind: "every", EveryMS: &interval}, "tick", false, "", "", "")

	cs.Start()
	time.Sleep(1500 * time.Millisecond)
	cs.Stop()

	log := cs.GetRunLog(job.ID, 50)
	if len(log) == 0 {
		t.Fatal("expected at least 1 run log entry from automatic execution")
	}
	if log[0].Status != "ok" {
		t.Fatalf("expected status 'ok', got %q", log[0].Status)
	}
}

// --- helpers ---

//go:fix inline
func ptrInt64(v int64) *int64 { return new(v) }
