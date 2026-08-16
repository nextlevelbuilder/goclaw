package reliability

import (
	"testing"
	"time"
)

func TestHealthRegistrySuccessAndFailure(t *testing.T) {
	now, _ := fakeClock(t)
	cb := newTestBreaker(now)
	reg := NewHealthRegistry(cb)

	reg.ObserveSuccess("pv", "m")
	reg.ObserveFailure("pv", "m", ErrProviderTimeout)
	reg.ObserveFailure("pv", "m", ErrProviderTimeout)

	st := reg.Status("pv", "m")
	if st.Attempts != 3 {
		t.Errorf("attempts=%d want 3", st.Attempts)
	}
	if st.Successes != 1 {
		t.Errorf("successes=%d want 1", st.Successes)
	}
	if st.TimeoutCount != 2 {
		t.Errorf("timeouts=%d want 2", st.TimeoutCount)
	}
	if st.CircuitState != CircuitDegraded {
		t.Errorf("circuit=%s want degraded", st.CircuitState)
	}
}

func TestHealthRegistryRateLimitSetsUntil(t *testing.T) {
	reg := NewHealthRegistry(nil)
	reg.ObserveFailure("pv", "m", ErrProviderRateLimited)
	st := reg.Status("pv", "m")
	if st.RateLimitUntil.IsZero() {
		t.Errorf("rate-limit should set RateLimitUntil")
	}
}

func TestHealthScoreHealthy(t *testing.T) {
	reg := NewHealthRegistry(nil)
	if s := reg.Score("pv", "m"); s != 1.0 {
		t.Errorf("no-signal score=%v want 1.0", s)
	}
	for i := 0; i < 10; i++ {
		reg.ObserveSuccess("pv", "m")
	}
	s := reg.Score("pv", "m")
	if s < 0.99 {
		t.Errorf("all-success score=%v want near 1.0", s)
	}
}

func TestHealthScoreDegradesWithFailures(t *testing.T) {
	reg := NewHealthRegistry(nil)
	for i := 0; i < 10; i++ {
		reg.ObserveFailure("pv", "m", ErrProviderTimeout)
	}
	// All timeouts: attempts=10, successes=0, timeouts=10 → score 0.
	s := reg.Score("pv", "m")
	if s != 0 {
		t.Errorf("all-timeout score=%v want 0", s)
	}
}

func TestHealthScoreOpenCircuitCrushed(t *testing.T) {
	now, _ := fakeClock(t)
	cb := newTestBreaker(now)
	reg := NewHealthRegistry(cb)
	// Pre-fill successes, then open the circuit.
	for i := 0; i < 10; i++ {
		reg.ObserveSuccess("pv", "m")
	}
	for i := 0; i < 5; i++ {
		reg.ObserveFailure("pv", "m", ErrProviderServerError)
	}
	s := reg.Score("pv", "m")
	if s >= 0.5 {
		t.Errorf("open-circuit score=%v should be crushed", s)
	}
}

// TestHealthScoreSingleCountsFailures: a timeout or empty output reduces the
// success ratio exactly once in Score — not again via a separate penalty.
// 10 attempts / 8 successes / 2 timeouts must score 0.8 (the 2 timeouts are
// already reflected in the success ratio).
func TestHealthScoreSingleCountsFailures(t *testing.T) {
	reg := NewHealthRegistry(nil)
	for i := 0; i < 8; i++ {
		reg.ObserveSuccess("pv", "m")
	}
	for i := 0; i < 2; i++ {
		reg.ObserveFailure("pv", "m", ErrProviderTimeout)
	}
	s := reg.Score("pv", "m")
	if want := 0.8; !closeEnough(s, want) {
		t.Errorf("score=%v want %v (timeouts must not be double-counted)", s, want)
	}
}

// TestHealthScorePenalizesStallAndToolErrorOnce: stream stalls and failed tool
// calls do NOT reduce the success ratio, so they are penalized explicitly —
// but only once each.
func TestHealthScorePenalizesStallAndToolError(t *testing.T) {
	reg := NewHealthRegistry(nil)
	for i := 0; i < 10; i++ {
		reg.ObserveSuccess("pv", "m")
	}
	reg.ObserveStreamStall("pv", "m")
	reg.ObserveToolResult("pv", "m", false)

	s := reg.Score("pv", "m")
	want := 1.0 - 0.25*(1.0/10.0) - 0.2*1.0 // success=1 minus stall + tool-error penalties
	if !closeEnough(s, want) {
		t.Errorf("score=%v want %v", s, want)
	}
}

// TestHealthRegistryModelCodesCounters verifies the additive wiring of the
// weak-model failure codes into the per-provider:model health counters.
func TestHealthRegistryModelCodesCounters(t *testing.T) {
	reg := NewHealthRegistry(nil)
	reg.ObserveFailure("pv", "m", ErrModelEmptyOutput)
	reg.ObserveFailure("pv", "m", ErrModelMalformedToolCall)
	reg.ObserveFailure("pv", "m", ErrModelInvalidJSON)
	reg.ObserveFailure("pv", "m", ErrModelRepeatedToolCall)
	reg.ObserveFailure("pv", "m", ErrModelPrematureCompletion)
	reg.ObserveFailure("pv", "m", ErrModelLooping)

	st := reg.Status("pv", "m")
	if st.EmptyOutputCount != 1 {
		t.Errorf("empty outputs=%d want 1", st.EmptyOutputCount)
	}
	if st.MalformedToolCallCount != 1 {
		t.Errorf("malformed tool calls=%d want 1", st.MalformedToolCallCount)
	}
	if st.InvalidJSONCount != 1 {
		t.Errorf("invalid JSONs=%d want 1", st.InvalidJSONCount)
	}
	if st.RepeatedToolCallCount != 1 {
		t.Errorf("repeated tool calls=%d want 1", st.RepeatedToolCallCount)
	}
	if st.PrematureCompleteCount != 1 {
		t.Errorf("premature completes=%d want 1", st.PrematureCompleteCount)
	}
	if st.LoopingCount != 1 {
		t.Errorf("loopings=%d want 1", st.LoopingCount)
	}
	if st.Attempts != 6 {
		t.Errorf("attempts=%d want 6", st.Attempts)
	}
}

func TestHealthToolErrorRate(t *testing.T) {
	reg := NewHealthRegistry(nil)
	reg.ObserveToolResult("pv", "m", true)
	reg.ObserveToolResult("pv", "m", true)
	reg.ObserveToolResult("pv", "m", false)
	st := reg.Status("pv", "m")
	if st.ToolCalls != 3 {
		t.Errorf("tool calls=%d want 3", st.ToolCalls)
	}
	if st.ToolErrors != 1 {
		t.Errorf("tool errors=%d want 1", st.ToolErrors)
	}
	if want := 1.0 / 3.0; !closeEnough(st.ToolErrorRate, want) {
		t.Errorf("tool error rate=%v want ~%v", st.ToolErrorRate, want)
	}
}

func TestStatusEmptyProvider(t *testing.T) {
	reg := NewHealthRegistry(nil)
	st := reg.Status("pv", "never-seen")
	if st.Attempts != 0 || st.ToolCalls != 0 {
		t.Errorf("untouched provider should have zero counters: %+v", st)
	}
}

func closeEnough(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.0001
}

func TestMetricsCountersAndSnapshot(t *testing.T) {
	m := NewMetrics()
	m.RecordLLMRequest()
	m.RecordLLMRequest()
	m.RecordLLMSuccess()
	m.RecordLLMRetry()
	m.RecordLLMRateLimited()
	m.RecordLLMServerError()
	m.RecordLLMTimeout()
	m.RecordLLMStreamStall()
	m.RecordLLMLoop()
	m.RecordLLMRepeatedToolCall()
	m.RecordLLMEmptyOutput()
	m.RecordLLMPrematureCompletion()
	m.RecordAgentRecovered()
	m.RecordAgentContinued()
	m.RecordPrematureCompletion()
	m.RecordLoopDetected()
	m.RecordLLMLatency(100 * time.Millisecond)

	s := m.Take()
	if s.LLMRequests != 2 || s.LLMSuccesses != 1 || s.LLMRetries != 1 {
		t.Errorf("counter snapshot mismatch: %+v", s)
	}
	if s.LLMRateLimited != 1 || s.LLMServerErrors != 1 || s.LLMTimeouts != 1 || s.LLMStreamStalls != 1 {
		t.Errorf("provider counters mismatch: %+v", s)
	}
	if s.LLMLoop != 1 || s.LLMRepeatedToolCalls != 1 || s.LLMEmptyOutputs != 1 || s.LLMPrematureCompletions != 1 {
		t.Errorf("model counters mismatch: %+v", s)
	}
	if s.AgentRecovered != 1 || s.AgentContinued != 1 || s.PrematureCompleted != 1 || s.LoopDetected != 1 {
		t.Errorf("agent counters mismatch: %+v", s)
	}
	if got := m.AvgLLMLatency(); got != 100*time.Millisecond {
		t.Errorf("avg latency=%v want 100ms", got)
	}
}

func TestMetricsFlushResets(t *testing.T) {
	m := NewMetrics()
	m.RecordLLMRequest()
	got := Snapshot{}
	RegisterSink(sinkFunc(func(s Snapshot) { got = s }))
	m.Flush()
	if got.LLMRequests != 1 {
		t.Errorf("flush should emit snapshot with 1 request, got %d", got.LLMRequests)
	}
	if m.Take().LLMRequests != 0 {
		t.Errorf("flush should reset counters")
	}
	RegisterSink(nopSink{})
}

type sinkFunc func(Snapshot)

func (f sinkFunc) Emit(s Snapshot) { f(s) }

func TestRateLimitCoordinatorSingleCooldown(t *testing.T) {
	now, advance := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now

	r.Record429("pv", "m", 10*time.Second)
	if d, ok := r.CooldownFor("pv", "m"); !ok || d <= 0 {
		t.Fatalf("expected active cooldown, got ok=%v d=%v", ok, d)
	}
	if _, ok := r.CooldownFor("pv", "m"); !ok {
		t.Fatalf("expected cooldown for second caller too")
	}
	advance(11 * time.Second)
	if _, ok := r.CooldownFor("pv", "m"); ok {
		t.Errorf("cooldown should have expired")
	}
}

func TestRateLimitCoordinatorWaiterCount(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	r.Record429("pv", "m", 5*time.Second)
	if w := r.Waiters("pv", "m"); w != 0 {
		t.Errorf("no waiters yet, want 0, got %d", w)
	}
	if d := r.ShouldWait("pv", "m"); d <= 0 {
		t.Errorf("expected positive wait, got %v", d)
	}
	if w := r.Waiters("pv", "m"); w != 1 {
		t.Errorf("waiter count=%d want 1", w)
	}
}

func TestRateLimitCoordinatorShouldWaitAndBegin(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	r.Record429("pv", "m", 5*time.Second)

	if d := r.ShouldWait("pv", "m"); d <= 0 {
		t.Errorf("expected positive wait, got %v", d)
	}
	if w := r.Waiters("pv", "m"); w != 1 {
		t.Errorf("waiter count=%d want 1", w)
	}
	r.BeginWait("pv", "m")
	if w := r.Waiters("pv", "m"); w != 0 {
		t.Errorf("waiter should decrement, got %d", w)
	}
}

func TestRateLimitCoordinatorClear(t *testing.T) {
	now, _ := fakeClock(t)
	r := NewRateLimitCoordinator(0)
	r.nowFn = now
	r.Record429("pv", "m", 5*time.Second)
	r.ClearCooldown("pv", "m")
	if _, ok := r.CooldownFor("pv", "m"); ok {
		t.Errorf("cooldown should be cleared")
	}
}
