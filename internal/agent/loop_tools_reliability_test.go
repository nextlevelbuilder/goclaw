package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// resetReliabilityMetricsForTests drains the process-wide metrics counters so
// tests can assert deltas. Mirrors the provider wiring-test pattern.
func resetReliabilityMetricsForTests() {
	reg := reliability.Default()
	if reg != nil && reg.Metrics != nil {
		reg.Metrics.Flush()
	}
}

func metricsSnapshot() reliability.Snapshot {
	reg := reliability.Default()
	if reg == nil || reg.Metrics == nil {
		return reliability.Snapshot{}
	}
	return reg.Metrics.Take()
}

// withDetectorState returns a runState whose loop detector already holds
// n identical (tool, args, result) records so the next processToolResult call
// crosses the warning threshold (3) — the detector counts records with
// matching argsHash AND matching resultHash.
func loopStatePrimed(streak int, toolName string, args map[string]any, resultForLLM string) *runState {
	rs := &runState{}
	for i := 0; i < streak; i++ {
		h := rs.loopDetector.record(toolName, args)
		rs.loopDetector.recordResult(h, resultForLLM)
	}
	return rs
}

// TestObserveToolLoopCriticalRecordsLoopMetric drives processToolResult across
// the critical loop threshold and asserts the LLMLoop counter delta. The
// detector's same-args+same-result detection fires at 5 identical records.
func TestObserveToolLoopCriticalRecordsLoopMetric(t *testing.T) {
	resetReliabilityMetricsForTests()
	before := metricsSnapshot()

	loop := newTestLoopForToolCallbacks(func(AgentEvent) {})
	rs := loopStatePrimed(4, "read_file", map[string]any{"path": "/tmp/x.txt"}, "same-content")
	req := &RunRequest{RunID: "run-loop-critical"}

	// The 5th identical call trips "critical". Arguments must match the primed
	// records so the detector counts all 5 as same-args + same-result.
	toolMsg, _, action := loop.processToolResult(
		context.Background(),
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{ID: "tc-5", Name: "read_file", Arguments: map[string]any{"path": "/tmp/x.txt"}},
		"read_file",
		&tools.Result{ForLLM: "same-content"},
		false,
	)
	if action != toolResultBreak {
		t.Fatalf("action = %v, want toolResultBreak on critical loop", action)
	}
	if toolMsg.Content == "" {
		t.Error("expected final-content message on critical loop")
	}

	after := metricsSnapshot()
	if after.LLMLoop-before.LLMLoop != 1 {
		t.Errorf("LLMLoop delta = %d, want 1 (got %+v before %+v after)", after.LLMLoop-before.LLMLoop, before, after)
	}
	// The critical classification must NOT record the warning-level counter.
	if after.LLMRepeatedToolCalls-before.LLMRepeatedToolCalls != 0 {
		t.Errorf("LLMRepeatedToolCalls delta = %d, want 0 for critical violation", after.LLMRepeatedToolCalls-before.LLMRepeatedToolCalls)
	}
	if !rs.loopKilled {
		t.Error("loopKilled not set on critical violation")
	}
}

// TestObserveToolLoopWarningRecordsRepeatedToolCallMetric drives the warning
// threshold (3 identical records) and asserts LLMRepeatedToolCall delta.
func TestObserveToolLoopWarningRecordsRepeatedToolCallMetric(t *testing.T) {
	resetReliabilityMetricsForTests()
	before := metricsSnapshot()

	loop := newTestLoopForToolCallbacks(func(AgentEvent) {})
	rs := loopStatePrimed(2, "read_file", map[string]any{"path": "/tmp/x.txt"}, "same-content")
	req := &RunRequest{RunID: "run-loop-warning"}

	// The 3rd identical call trips "warning".
	_, warningMsgs, action := loop.processToolResult(
		context.Background(),
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{ID: "tc-3", Name: "read_file", Arguments: map[string]any{"path": "/tmp/x.txt"}},
		"read_file",
		&tools.Result{ForLLM: "same-content"},
		false,
	)
	if action != toolResultWarning {
		t.Fatalf("action = %v, want toolResultWarning on warning loop", action)
	}
	if len(warningMsgs) == 0 {
		t.Fatal("expected injected warning message on warning loop")
	}

	after := metricsSnapshot()
	if after.LLMRepeatedToolCalls-before.LLMRepeatedToolCalls != 1 {
		t.Errorf("LLMRepeatedToolCalls delta = %d, want 1", after.LLMRepeatedToolCalls-before.LLMRepeatedToolCalls)
	}
	if after.LLMLoop-before.LLMLoop != 0 {
		t.Errorf("LLMLoop delta = %d, want 0 for warning violation", after.LLMLoop-before.LLMLoop)
	}
	if rs.loopKilled {
		t.Error("loopKilled must stay false on warning violation")
	}
}

// TestObserveToolLoopWrongLevelNoOp verifies the observability wiring is a
// no-op for non-loop detectors: a plain result records no metrics.
func TestObserveToolLoopWrongLevelNoOp(t *testing.T) {
	resetReliabilityMetricsForTests()
	before := metricsSnapshot()

	loop := newTestLoopForToolCallbacks(func(AgentEvent) {})
	rs := loopStatePrimed(2, "read_file", map[string]any{"path": "/tmp/x.txt"}, "diff-content")
	req := &RunRequest{RunID: "run-loop-none"}

	// Different content each time → no violation.
	_, _, action := loop.processToolResult(
		context.Background(),
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{ID: "tc-3", Name: "read_file", Arguments: map[string]any{"path": "/tmp/x.txt"}},
		"read_file",
		&tools.Result{ForLLM: "new-content"},
		false,
	)
	if action != toolResultContinue {
		t.Fatalf("action = %v, want toolResultContinue without violation", action)
	}
	after := metricsSnapshot()
	if after.LLMLoop != before.LLMLoop || after.LLMRepeatedToolCalls != before.LLMRepeatedToolCalls {
		t.Errorf("metrics changed without violation: before %+v after %+v", before, after)
	}
}

// TestObserveToolLoopSameResultCycle covers the detectSameResult path: the
// same tool returning identical results with DIFFERENT args trips warning at
// 4 identical result hashes.
func TestObserveToolLoopSameResultCycle(t *testing.T) {
	resetReliabilityMetricsForTests()
	before := metricsSnapshot()

	loop := newTestLoopForToolCallbacks(func(AgentEvent) {})
	rs := &runState{}
	// 3 records with distinct args but identical content.
	for i := 0; i < 3; i++ {
		h := rs.loopDetector.record("read_file", map[string]any{"path": "/tmp/x.txt", "i": i})
		rs.loopDetector.recordResult(h, "same-result")
	}
	req := &RunRequest{RunID: "run-same-result"}

	// 4th identical result → warning. Args differ (new record has no "i" key)
	// so detectSameResult (different-args path) applies.
	_, warningMsgs, action := loop.processToolResult(
		context.Background(),
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{ID: "tc-4", Name: "read_file", Arguments: map[string]any{"path": "/tmp/x.txt"}},
		"read_file",
		&tools.Result{ForLLM: "same-result"},
		false,
	)
	if action != toolResultWarning {
		t.Fatalf("action = %v, want toolResultWarning for same-result cycle", action)
	}
	if len(warningMsgs) == 0 {
		t.Fatal("expected injected warning message for same-result cycle")
	}
	after := metricsSnapshot()
	if after.LLMRepeatedToolCalls-before.LLMRepeatedToolCalls != 1 {
		t.Errorf("LLMRepeatedToolCalls delta = %d, want 1", after.LLMRepeatedToolCalls-before.LLMRepeatedToolCalls)
	}
}
