//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestRouter_AbortRun_NotFound verifies that aborting a non-existent run
// returns NotFound=true.
func TestRouter_AbortRun_NotFound(t *testing.T) {
	t.Parallel()

	router := agent.NewRouter()
	result := router.AbortRun("nonexistent-run-id", "")

	if !result.NotFound {
		t.Errorf("expected NotFound=true, got %+v", result)
	}
	if result.Stopped || result.Forced || result.AlreadyAborting {
		t.Errorf("expected only NotFound=true, got %+v", result)
	}
}

// TestRouter_AbortRun_Unauthorized verifies that aborting with a mismatched
// sessionKey returns Unauthorized=true.
func TestRouter_AbortRun_Unauthorized(t *testing.T) {
	t.Parallel()

	router := agent.NewRouter()

	// Register a run with sessionKey "session-A"
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	router.RegisterRun(context.Background(), "run-1", "session-A", "agent-1", cancel)

	// Try to abort with a different sessionKey
	result := router.AbortRun("run-1", "session-B")

	if !result.Unauthorized {
		t.Errorf("expected Unauthorized=true, got %+v", result)
	}
	if result.Stopped || result.Forced || result.NotFound {
		t.Errorf("expected only Unauthorized=true, got %+v", result)
	}
}

// TestRouter_AbortRun_Concurrent_OnlyOneStops verifies that when multiple
// AbortRun calls race on the same run, exactly one succeeds (Stopped or Forced),
// and the rest see AlreadyAborting.
func TestRouter_AbortRun_Concurrent_OnlyOneStops(t *testing.T) {
	t.Parallel()

	router := agent.NewRouter()

	// Register a run that will exit after 50ms
	_, cancel := context.WithCancel(context.Background())
	runID := "run-1"
	sessionKey := "session-1"
	_ = router.RegisterRun(context.Background(), runID, sessionKey, "agent-1", cancel)

	// Goroutine closes Done after 50ms (simulating normal graceful exit)
	go func() {
		time.Sleep(50 * time.Millisecond)
		router.UnregisterRun(runID) // this closes Done
	}()

	// Launch 100 concurrent AbortRun calls
	resultsCh := make(chan agent.AbortResult, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := router.AbortRun(runID, sessionKey)
			resultsCh <- result
		}()
	}

	// Wait for all to complete
	wg.Wait()
	close(resultsCh)

	// Collect and count results
	var results []agent.AbortResult
	for r := range resultsCh {
		results = append(results, r)
	}

	var stoppedCount, forcedCount, alreadyAbortingCount, notFoundCount int
	for _, r := range results {
		if r.Stopped {
			stoppedCount++
		}
		if r.Forced {
			forcedCount++
		}
		if r.AlreadyAborting {
			alreadyAbortingCount++
		}
		if r.NotFound {
			notFoundCount++
		}
	}

	t.Logf("Results: stopped=%d, forced=%d, alreadyAborting=%d, notFound=%d, total=%d",
		stoppedCount, forcedCount, alreadyAbortingCount, notFoundCount, len(results))

	// Verify exactly one succeeded (Stopped or Forced)
	successCount := stoppedCount + forcedCount
	if successCount != 1 {
		t.Errorf("expected exactly 1 success (Stopped|Forced), got %d", successCount)
	}

	// Verify the rest are AlreadyAborting (or NotFound if race)
	expectedAlreadyAborting := len(results) - 1
	if alreadyAbortingCount+notFoundCount != expectedAlreadyAborting {
		t.Errorf("expected %d AlreadyAborting+NotFound, got %d+%d",
			expectedAlreadyAborting, alreadyAbortingCount, notFoundCount)
	}

	// Verify total count
	if len(results) != 100 {
		t.Errorf("expected 100 results, got %d", len(results))
	}
}

// TestRouter_AbortRun_ForcesAfter3s verifies that when a run's goroutine
// never exits, AbortRun waits ~3s then returns Forced=true.
// Also verifies the mock TraceCollector is called with status="cancelled".
func TestRouter_AbortRun_ForcesAfter3s(t *testing.T) {
	t.Parallel()

	router := agent.NewRouter()
	collector := &mockTraceCollector{}
	router.SetTraceCollector(collector)

	// Use a tenant-scoped context so forceMarkTraceAborted carries the tenant to FinishTrace.
	testTenantID := uuid.New()
	tenantCtx := store.WithTenantID(context.Background(), testTenantID)

	// Register a run
	_, cancel := context.WithCancel(tenantCtx)
	runID := "run-1"
	sessionKey := "session-1"
	agentID := "agent-1"
	router.RegisterRun(tenantCtx, runID, sessionKey, agentID, cancel)

	// Set a trace ID
	traceID := uuid.New()
	router.SetRunTraceID(runID, traceID)

	// DO NOT close Done — the goroutine will be "stuck"
	// (We never call router.UnregisterRun, so Done stays open)

	// Call AbortRun and time it
	start := time.Now()
	result := router.AbortRun(runID, sessionKey)
	elapsed := time.Since(start)

	// Verify: returned Forced=true
	if !result.Forced {
		t.Errorf("expected Forced=true, got %+v", result)
	}
	if result.Stopped || result.NotFound || result.Unauthorized || result.AlreadyAborting {
		t.Errorf("expected only Forced=true, got %+v", result)
	}

	// Verify: elapsed time is in the range [2.9s, 3.5s]
	// (Allow some variance for slow test runners)
	minElapsed := 2900 * time.Millisecond
	maxElapsed := 3500 * time.Millisecond
	if elapsed < minElapsed || elapsed > maxElapsed {
		t.Errorf("expected elapsed time in [2.9s, 3.5s], got %v", elapsed)
	}
	t.Logf("AbortRun forced after %v", elapsed)

	// Verify: trace collector was called with status="cancelled" and correct tenant
	if collector.FinishCallCount() == 0 {
		t.Error("expected FinishTrace to be called, got 0 calls")
	} else {
		call := collector.LastFinishTrace()
		if call.Status != "cancelled" {
			t.Errorf("expected status='cancelled', got '%s'", call.Status)
		}
		if call.TraceID != traceID {
			t.Errorf("expected traceID=%s, got %s", traceID, call.TraceID)
		}
		// Verify tenant was propagated to the FinishTrace ctx (FIX 1 regression check).
		if call.TenantID != testTenantID {
			t.Errorf("expected tenantID=%s in FinishTrace ctx, got %s", testTenantID, call.TenantID)
		}
	}
}
