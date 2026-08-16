package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// stubProvider implements providers.Provider so the empty-output wiring test can
// assert the health key is the concrete provider:model pair.
type stubProvider struct {
	response string
	err      error
}

func (s *stubProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &providers.ChatResponse{Content: s.response}, nil
}

func (s *stubProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: s.response}, nil
}

func (s *stubProvider) DefaultModel() string { return "stub-model" }
func (s *stubProvider) Name() string          { return "stub" }

// contGuardState builds a RunState positioned after ThinkStage's final-answer
// path: an empty final response with the empty-reply nudge exhausted.
func contGuardState() *RunState {
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "",
		FinishReason: "stop",
	}
	state.Think.EmptyReplyRetries = maxEmptyReplyRetries
	return state
}

// enableContinuationGate installs a fresh reliability bundle with the
// premature-completion gate enabled and returns its Metrics for assertions.
func enableContinuationGate(t *testing.T) *reliability.Metrics {
	t.Helper()
	rt := reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	rt.PrematureCompletion.Enabled = true
	return rt.Metrics
}

// disableContinuationGate installs a fresh bundle with the gate disabled
// (the production default — zero value).
func disableContinuationGate(t *testing.T) {
	t.Helper()
	reliability.Configure(reliability.DefaultCircuitOptions(), 0)
}

// TestContinuationGate_FiresWhenNoToolsAndNoDeliverable verifies the gate asks
// for one more iteration when the model broke the loop with an empty final
// answer, zero tool iterations ran, and the run has no deliverable output.
func TestContinuationGate_FiresWhenNoToolsAndNoDeliverable(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = false, want true (premature completion detected)")
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
}

// TestContinuationGate_DisabledByDefault verifies the gate is inert when the
// reliability runtime carries the zero-value config (production default).
func TestContinuationGate_DisabledByDefault(t *testing.T) {
	disableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (gate disabled by default)")
	}
}

// TestContinuationGate_NoFireWithContent verifies the gate never fires when the
// final answer carries content: the model delivered a real reply.
func TestContinuationGate_NoFireWithContent(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	state.Think.LastResponse.Content = "Here is the answer."

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (content present)")
	}
}

// TestContinuationGate_NoFireWithDeliverable verifies deliverable output
// (media, forwarded media, content suffix) suppresses the gate — the deliverable
// IS the reply.
func TestContinuationGate_NoFireWithDeliverable(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)

	for name, mutate := range map[string]func(*RunState){
		"media results":      func(s *RunState) { s.Tool.MediaResults = []MediaResult{{Path: "/tmp/img.png", ContentType: "image/png"}} },
		"forwarded media":    func(s *RunState) { s.Input.ForwardMedia = []bus.MediaFile{{Path: "/tmp/doc.pdf", MimeType: "application/pdf"}} },
		"content suffix":     func(s *RunState) { s.Input.ContentSuffix = "\n![img](/media/img.png)" },
	} {
		t.Run(name, func(t *testing.T) {
			state := contGuardState()
			mutate(state)
			if err := stage.Execute(context.Background(), state); err != nil {
				t.Fatalf("Execute() error: %v", err)
			}
			if state.Observe.ContinueAfterFinal {
				t.Errorf("ContinueAfterFinal = true, want false (%s is a deliverable)", name)
			}
		})
	}
}

// TestContinuationGate_NoFireWithToolIterations verifies a run that used at
// least one tool iteration is not gated — the model already worked.
func TestContinuationGate_NoFireWithToolIterations(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	state.Tool.TotalToolCalls = 1

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (tool iteration ran)")
	}
}

// TestContinuationGate_NoFireOnLastIteration verifies the gate never fires when
// the final iteration is in progress — no iteration remains to continue with.
func TestContinuationGate_NoFireOnLastIteration(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 3}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	state.Iteration = 2 // last of 3

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (last iteration)")
	}
}

// TestContinuationGate_NoFireWhenAlreadyContinued verifies the gate never fires
// twice in a run: the per-run marker set by a previous gate firing means the
// continuation did not produce a real answer — the run now completes.
func TestContinuationGate_NoFireWhenAlreadyContinued(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	state.Observe.ContinuationGateFired = true

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (gate already fired this run)")
	}
}

// TestContinuationGate_NoFireWhileNudgeInFlight verifies the gate leaves the
// bounded empty-reply nudge flow alone while the nudge still has budget.
func TestContinuationGate_NoFireWhileNudgeInFlight(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	state.Think.EmptyReplyRetries = 1 // maxEmptyReplyRetries-1: still one nudge left

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (nudge still in flight)")
	}
}

// TestContinuationGate_ToolIterationSkipped verifies the gate ignores tool
// iterations (ThinkStage returned Continue) — only the final-answer path is
// inspected.
func TestContinuationGate_ToolIterationSkipped(t *testing.T) {
	enableContinuationGate(t)
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	state.Think.LastResponse.ToolCalls = []providers.ToolCall{{Name: "read_file", Arguments: map[string]any{"path": "/tmp/a"}}}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true, want false (tool iteration)")
	}
}

// TestContinuationGate_ContinueAfterFinalPath verifies the full pipeline flow:
// an empty final answer on the first iteration fires the gate, the pipeline
// runs another iteration, and the model's second answer completes the run.
// Mirrors the pipeline_test.go:197-249 late-injection pattern.
func TestContinuationGate_ContinueAfterFinalPath(t *testing.T) {
	enableContinuationGate(t)
	deps := PipelineDeps{
		Config: PipelineConfig{MaxIterations: 3, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "", FinishReason: "stop"}, nil
		},
	}
	p := NewPipeline(
		nil,
		[]Stage{NewThinkStage(&deps), NewContinuationGate(&deps), NewObserveStage(&deps)},
		nil,
		deps,
	)

	state := buildMinimalRunState()
	result, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal still set after pipeline run")
	}
	// The gate gave the model one extra turn; the second final answer is empty
	// too, so the run breaks with the fallback placeholder.
	if result.Content != "" {
		t.Errorf("result content = %q, want empty", result.Content)
	}
}

// TestContinuationGate_SecondIterationGatedOnce verifies the gate does not
// cascade: when the continuation iteration also ends empty, the per-run marker
// stops the gate from re-firing, so the run exits long before MaxIterations.
// Iteration timeline (MaxIterations=5): nudge (iter 0), nudge (iter 1),
// gate fires (iter 2), run exits (iter 3) — never reaching iterations 4-5.
func TestContinuationGate_SecondIterationGatedOnce(t *testing.T) {
	enableContinuationGate(t)
	deps := PipelineDeps{
		Config: PipelineConfig{MaxIterations: 5, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "", FinishReason: "stop"}, nil
		},
	}
	p := NewPipeline(
		nil,
		[]Stage{NewThinkStage(&deps), NewContinuationGate(&deps), NewObserveStage(&deps)},
		nil,
		deps,
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !state.Observe.ContinuationGateFired {
		t.Error("ContinuationGateFired = false, want true (gate fired once)")
	}
	if state.Iteration != 3 {
		t.Errorf("Iteration = %d, want 3 (2 nudges + gate + 1 exit iteration; no cascade to 4)", state.Iteration)
	}
}

// --- Empty-output observability wiring (C3) ---

// TestThinkStage_EmptyOutputWire_CounterDelta verifies the exhausted empty-reply
// path records the empty-output event in the reliability layer: the health
// registry's emptyOutputs counter increments for the provider:model pair and
// the metrics empty-output counter increments.
func TestThinkStage_EmptyOutputWire_CounterDelta(t *testing.T) {
	rt := reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(5)
	state.Think.EmptyReplyRetries = maxEmptyReplyRetries
	state.Provider = &stubProvider{response: ""}
	state.Model = "stub-model" // health registry key is provider:model

	before := rt.Metrics.Take()
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop (nudge exhausted)", stage.Result())
	}

	after := rt.Metrics.Take()
	if got := after.LLMEmptyOutputs - before.LLMEmptyOutputs; got != 1 {
		t.Errorf("LLMEmptyOutputs delta = %d, want 1", got)
	}
	health := rt.Health.Status("stub", "stub-model")
	if health.EmptyOutputCount != 1 {
		t.Errorf("health EmptyOutputCount = %d, want 1", health.EmptyOutputCount)
	}
	if health.Attempts != 1 {
		t.Errorf("health Attempts = %d, want 1 (ObserveFailure counts an attempt)", health.Attempts)
	}
}

// TestThinkStage_EmptyOutputWire_NudgePathNoObserve verifies the observability
// fires only when the nudge is exhausted: while a nudge is still possible the
// reliability layer stays silent (the run may still recover).
func TestThinkStage_EmptyOutputWire_NudgePathNoObserve(t *testing.T) {
	rt := reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(0)
	state.Provider = &stubProvider{response: ""}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (nudge)", stage.Result())
	}
	after := rt.Metrics.Take()
	if after.LLMEmptyOutputs != 0 {
		t.Errorf("LLMEmptyOutputs = %d, want 0 (nudge still in flight)", after.LLMEmptyOutputs)
	}
}

// TestThinkStage_EmptyOutputWire_MediaOnlyNoObserve verifies media-only runs
// break without observing an empty-output event — the media is the deliverable.
func TestThinkStage_EmptyOutputWire_MediaOnlyNoObserve(t *testing.T) {
	rt := reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(5)
	state.Think.EmptyReplyRetries = maxEmptyReplyRetries
	state.Tool.MediaResults = []MediaResult{{Path: "/tmp/img.png", ContentType: "image/png"}}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop (media-only)", stage.Result())
	}
	after := rt.Metrics.Take()
	if after.LLMEmptyOutputs != 0 {
		t.Errorf("LLMEmptyOutputs = %d, want 0 (media-only deliverable)", after.LLMEmptyOutputs)
	}
}

// TestThinkStage_EmptyOutputWire_LastIterationObserves verifies the last-iteration
// empty final (no nudge possible) still observes the event — the user gets the
// fallback and reliability sees the empty output.
func TestThinkStage_EmptyOutputWire_LastIterationObserves(t *testing.T) {
	rt := reliability.Configure(reliability.DefaultCircuitOptions(), 0)
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 3, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(2) // last of 3 iterations
	state.Provider = &stubProvider{response: ""}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop", stage.Result())
	}
	after := rt.Metrics.Take()
	if after.LLMEmptyOutputs != 1 {
		t.Errorf("LLMEmptyOutputs = %d, want 1 (empty final on last iteration)", after.LLMEmptyOutputs)
	}
}

// TestContinuationGate_Fires_NilSafety verifies both gate and empty-output
// wiring tolerate a nil reliability runtime (never panics).
func TestContinuationGate_NilRuntimeSafety(t *testing.T) {
	// Pipeline tests that never Configure() still hit the lazily-built default
	// bundle via Default() — this test guards the nil-deref contract.
	r := reliability.Default()
	if r == nil {
		t.Fatal("Default() returned nil — nil-safety of stage code cannot be exercised")
	}
	deps := &PipelineDeps{Config: PipelineConfig{MaxIterations: 5}}
	stage := NewContinuationGate(deps)
	state := contGuardState()
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	// Default bundle has PrematureCompletion zero value → gate inert.
	if state.Observe.ContinueAfterFinal {
		t.Error("ContinueAfterFinal = true with default bundle, want false (disabled)")
	}
}

// TestContinuationGate_EmptyReplyHintUnchanged guards the empty-reply nudge
// message contract used by existing think-stage tests.
func TestContinuationGate_EmptyReplyHintUnchanged(t *testing.T) {
	if !strings.Contains(emptyReplyHint, "Give the user your final answer now.") {
		t.Errorf("emptyReplyHint = %q, want the stable nudge text", emptyReplyHint)
	}
}
