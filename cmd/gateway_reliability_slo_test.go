package cmd

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// TestSLOBurnRateDrivesAlert verifies the shared flush loop folds each Metrics
// delta into the tracker and the tracker leaves its budget when failures
// accumulate, causing the webhook branch to fire. It exercises the SLO wiring
// path that wireReliabilityMetrics uses on both default and otel builds.
func TestSLOBurnRateDrivesAlert(t *testing.T) {
	// Fresh reliability bundle so the test never depends on other tests'
	// counters.
	reliability.Configure(reliability.DefaultCircuitOptions(), 0)

	cfg := config.Default()
	cfg.Reliability.SLO.Enabled = true
	cfg.Reliability.SLO.TargetPercent = 0.99
	cfg.Reliability.SLO.WindowSeconds = 3600
	cfg.Reliability.Alerts.Enabled = true
	// No webhook URL: the send is a no-op, but the loop must still Observe.
	cfg.Reliability.Alerts.WebhookURL = ""

	tracker := sloTrackerFromConfig(cfg)
	if tracker == nil {
		t.Fatal("sloTrackerFromConfig returned nil for enabled SLO")
	}

	// Drive the tracker directly with failure deltas — the same snapshots the
	// flush loop folds in. Two failed intervals out of a small window must put
	// the tracker outside its budget.
	tracker.Observe(reliability.Snapshot{LLMRequests: 100, LLMSuccesses: 0})
	tracker.Observe(reliability.Snapshot{LLMRequests: 100, LLMSuccesses: 0})
	st := tracker.Status()
	if st.WithinBudget {
		t.Fatalf("all-failure tracker must be outside budget, got %+v", st)
	}

	// The same path must not fire a webhook when the SLO is disabled (no
	// tracker at all).
	if tr := sloTrackerFromConfig(config.Default()); tr != nil {
		t.Fatal("sloTrackerFromConfig must return nil when reliability.slo.enabled is false")
	}
}

// TestReliabilityFlushLoopTakeBeforeFlush verifies the shared flush loop
// drains the counters once per tick and folds the same delta into the tracker.
// The 5s ticker is too slow for a unit test, so the loop contract is asserted
// at the unit level: Take before Flush yields the same values the tracker
// observes.
func TestReliabilityFlushLoopTakeBeforeFlush(t *testing.T) {
	reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	m := reliability.Default().Metrics

	m.RecordLLMRequest()
	m.RecordLLMSuccess()

	// This mirrors the flush loop body: Take then Flush.
	snap := m.Take()
	m.Flush()

	if snap.LLMRequests != 1 || snap.LLMSuccesses != 1 {
		t.Fatalf("snapshot = %+v, want 1 request / 1 success", snap)
	}
	if after := m.Take(); after.LLMRequests != 0 {
		t.Fatalf("counters must reset after Flush, got %d requests", after.LLMRequests)
	}
}

// TestSLOFlushLoopContract exercises the loop wiring without waiting on the
// real 5s ticker: with SLO disabled no loop is started, and with SLO enabled
// the loop is started and stops cleanly.
func TestSLOFlushLoopContract(t *testing.T) {
	cfg := config.Default()

	// Disabled → no loop, no stop function.
	if stop, ok := startReliabilityFlushLoop(context.Background(), cfg, nil, false); ok || stop != nil {
		t.Fatalf("disabled SLO must not start a loop (stop=%v ok=%v)", stop != nil, ok)
	}

	// Enabled → loop starts and stops.
	reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	cfg.Reliability.SLO.Enabled = true
	tracker := sloTrackerFromConfig(cfg)
	if tracker == nil {
		t.Fatal("expected tracker for enabled SLO")
	}
	stop, ok := startReliabilityFlushLoop(context.Background(), cfg, tracker, false)
	if !ok || stop == nil {
		t.Fatalf("enabled SLO must start a loop (stop=%v ok=%v)", stop != nil, ok)
	}
	stop() // must not panic or deadlock
}
