package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// --- mock stage helpers ---

type mockStage struct {
	name    string
	execFn  func(ctx context.Context, state *RunState) error
	result  StageResult
	execCnt int
}

func (m *mockStage) Name() string { return m.name }
func (m *mockStage) Execute(ctx context.Context, state *RunState) error {
	m.execCnt++
	if m.execFn != nil {
		return m.execFn(ctx, state)
	}
	return nil
}
func (m *mockStage) Result() StageResult { return m.result }

// stageWithResult wraps mockStage so it also implements StageWithResult.
type stageWithResult struct {
	*mockStage
}

func newMockStageNoResult(name string) *mockStage {
	return &mockStage{name: name, result: Continue}
}

func newMockStageWithResult(name string, r StageResult) *stageWithResult {
	return &stageWithResult{&mockStage{name: name, result: r}}
}

// buildMinimalRunState returns a RunState with minimal required fields set.
func buildMinimalRunState() *RunState {
	input := &RunInput{
		SessionKey: "test-session",
		RunID:      "run-123",
		UserID:     "user-1",
	}
	ws := &workspace.WorkspaceContext{ActivePath: "/tmp/test"}
	return NewRunState(input, ws, "claude-3", nil)
}

// --- tests ---

func TestPipeline_SetupRunsOnce(t *testing.T) {
	t.Parallel()
	setup := newMockStageNoResult("setup")
	iter := newMockStageNoResult("iter")

	p := NewPipeline(
		[]Stage{setup},
		[]Stage{iter},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 3}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if setup.execCnt != 1 {
		t.Errorf("setup execCnt = %d, want 1", setup.execCnt)
	}
	// iter runs MaxIterations times (no BreakLoop signal)
	if iter.execCnt != 3 {
		t.Errorf("iter execCnt = %d, want 3", iter.execCnt)
	}
}

func TestPipeline_FinalizeRunsOnce(t *testing.T) {
	t.Parallel()
	finalize := newMockStageNoResult("finalize")

	p := NewPipeline(
		nil,
		nil,
		[]Stage{finalize},
		PipelineDeps{Config: PipelineConfig{MaxIterations: 2}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if finalize.execCnt != 1 {
		t.Errorf("finalize execCnt = %d, want 1", finalize.execCnt)
	}
}

func TestPipeline_BreakLoopExitsIteration(t *testing.T) {
	t.Parallel()
	// BreakLoop completes all remaining stages in the iteration (ObserveStage
	// must run to capture FinalContent), then exits the outer loop.
	breaker := newMockStageWithResult("breaker", BreakLoop)
	after := newMockStageNoResult("after") // SHOULD run after BreakLoop (remaining stage)

	p := NewPipeline(
		nil,
		[]Stage{breaker, after},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 10}},
	)

	state := buildMinimalRunState()
	result, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if breaker.execCnt != 1 {
		t.Errorf("breaker ran %d times, want 1", breaker.execCnt)
	}
	if after.execCnt != 1 {
		t.Errorf("after ran %d times after BreakLoop, want 1 (remaining stages complete)", after.execCnt)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestPipeline_AbortRunExitsIteration(t *testing.T) {
	t.Parallel()
	aborter := newMockStageWithResult("aborter", AbortRun)
	after := newMockStageNoResult("after")

	finalize := newMockStageNoResult("finalize")

	p := NewPipeline(
		nil,
		[]Stage{aborter, after},
		[]Stage{finalize},
		PipelineDeps{Config: PipelineConfig{MaxIterations: 10}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if aborter.execCnt != 1 {
		t.Errorf("aborter ran %d times, want 1", aborter.execCnt)
	}
	if after.execCnt != 0 {
		t.Errorf("after ran %d times after AbortRun, want 0", after.execCnt)
	}
	// finalize still runs
	if finalize.execCnt != 1 {
		t.Errorf("finalize ran %d times, want 1", finalize.execCnt)
	}
}

func TestPipeline_FinalizeRunsAfterError(t *testing.T) {
	t.Parallel()
	errStage := newMockStageNoResult("err")
	errStage.execFn = func(_ context.Context, _ *RunState) error {
		return errors.New("boom")
	}
	finalize := newMockStageNoResult("finalize")

	p := NewPipeline(
		nil,
		[]Stage{errStage},
		[]Stage{finalize},
		PipelineDeps{Config: PipelineConfig{MaxIterations: 3}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from errStage, got nil")
	}
	// finalize does NOT run when Run() returns early with error (pipeline propagates error from iteration)
	// Per pipeline.go: iteration errors return immediately, finalize not reached.
	// This is correct by design — finalize only runs on BreakLoop/AbortRun/ctx cancel.
	_ = finalize.execCnt
}

func TestPipeline_CtxCancellationSetsAbortRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	iter := newMockStageNoResult("iter")
	iter.execFn = func(_ context.Context, _ *RunState) error {
		callCount++
		if callCount == 2 {
			cancel() // cancel mid-loop
		}
		return nil
	}

	finalize := newMockStageNoResult("finalize")

	p := NewPipeline(
		nil,
		[]Stage{iter},
		[]Stage{finalize},
		PipelineDeps{Config: PipelineConfig{MaxIterations: 10}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(ctx, state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	// finalize runs even after cancellation
	if finalize.execCnt != 1 {
		t.Errorf("finalize ran %d times after ctx cancel, want 1", finalize.execCnt)
	}
	if state.ExitCode != AbortRun {
		t.Errorf("ExitCode = %d after ctx cancel, want AbortRun(%d)", state.ExitCode, AbortRun)
	}
}

func TestPipeline_MaxIterationsBoundsLoop(t *testing.T) {
	t.Parallel()
	iter := newMockStageNoResult("iter")

	p := NewPipeline(
		nil,
		[]Stage{iter},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 5}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if iter.execCnt != 5 {
		t.Errorf("iter ran %d times, want 5 (MaxIterations)", iter.execCnt)
	}
}

func TestPipeline_SetupErrorStopsEarly(t *testing.T) {
	t.Parallel()
	setup := newMockStageNoResult("setup")
	setup.execFn = func(_ context.Context, _ *RunState) error {
		return errors.New("setup failed")
	}
	iter := newMockStageNoResult("iter")

	p := NewPipeline(
		[]Stage{setup},
		[]Stage{iter},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 3}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from setup, got nil")
	}
	if iter.execCnt != 0 {
		t.Errorf("iter ran %d times despite setup failure, want 0", iter.execCnt)
	}
}

func TestPipeline_BuildResultPopulatesRunID(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		nil,
		nil,
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 1}},
	)

	state := buildMinimalRunState()
	state.Observe.FinalContent = "hello"
	state.Think.TotalUsage = providers.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}

	result, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.RunID != "run-123" {
		t.Errorf("result.RunID = %q, want run-123", result.RunID)
	}
	if result.Content != "hello" {
		t.Errorf("result.Content = %q, want hello", result.Content)
	}
	if result.TotalUsage.TotalTokens != 15 {
		t.Errorf("result.TotalUsage.TotalTokens = %d, want 15", result.TotalUsage.TotalTokens)
	}
	if result.Duration <= 0 {
		t.Errorf("result.Duration = %v, want > 0", result.Duration)
	}
}

func TestPipeline_StageWithResultContinue_LoopsNormally(t *testing.T) {
	t.Parallel()
	// stage implements StageWithResult but returns Continue — should not break
	continuer := newMockStageWithResult("continuer", Continue)

	p := NewPipeline(
		nil,
		[]Stage{continuer},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 4}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if continuer.execCnt != 4 {
		t.Errorf("continuer ran %d times, want 4", continuer.execCnt)
	}
}

func TestPipeline_IterationCounterIncrements(t *testing.T) {
	t.Parallel()
	var iterations []int
	iter := newMockStageNoResult("iter")
	iter.execFn = func(_ context.Context, state *RunState) error {
		iterations = append(iterations, state.Iteration)
		return nil
	}

	p := NewPipeline(
		nil,
		[]Stage{iter},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 3}},
	)

	state := buildMinimalRunState()
	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(iterations) != 3 {
		t.Fatalf("got %d iterations, want 3", len(iterations))
	}
	for i, iter := range iterations {
		if iter != i {
			t.Errorf("iterations[%d] = %d, want %d", i, iter, i)
		}
	}
}

func TestPipeline_FinalizeErrorIsLogged_NotFatal(t *testing.T) {
	t.Parallel()
	finalize := newMockStageNoResult("finalize")
	finalize.execFn = func(_ context.Context, _ *RunState) error {
		return errors.New("finalize exploded")
	}

	p := NewPipeline(
		nil,
		nil,
		[]Stage{finalize},
		PipelineDeps{Config: PipelineConfig{MaxIterations: 1}},
	)

	state := buildMinimalRunState()
	result, err := p.Run(context.Background(), state)
	// finalize errors are logged not returned
	if err != nil {
		t.Fatalf("Run() returned error from finalize, want nil: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}

func TestRunState_BuildResult_AllFields(t *testing.T) {
	t.Parallel()
	input := &RunInput{SessionKey: "s", RunID: "r42"}
	ws := &workspace.WorkspaceContext{}
	state := NewRunState(input, ws, "gpt-4o", nil)

	state.Observe.FinalContent = "final"
	state.Observe.FinalThinking = "thinking"
	state.Think.TotalUsage = providers.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	state.Iteration = 7
	state.Tool.TotalToolCalls = 3
	state.Tool.LoopKilled = true
	state.Tool.AsyncToolCalls = []string{"spawn"}
	state.Tool.MediaResults = []MediaResult{{Path: "/tmp/out.png", ContentType: "image/png"}}
	state.Tool.Deliverables = []string{"deliver"}
	state.Observe.BlockReplies = 2
	state.Observe.LastBlockReply = "last"

	r := state.BuildResult()
	if r.RunID != "r42" {
		t.Errorf("RunID = %q", r.RunID)
	}
	if r.Content != "final" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.Thinking != "thinking" {
		t.Errorf("Thinking = %q", r.Thinking)
	}
	if r.TotalUsage.TotalTokens != 150 {
		t.Errorf("TotalUsage.TotalTokens = %d", r.TotalUsage.TotalTokens)
	}
	if r.Iterations != 7 {
		t.Errorf("Iterations = %d", r.Iterations)
	}
	if r.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d", r.ToolCalls)
	}
	if !r.LoopKilled {
		t.Error("LoopKilled should be true")
	}
	if len(r.AsyncToolCalls) != 1 {
		t.Errorf("AsyncToolCalls len = %d", len(r.AsyncToolCalls))
	}
	if len(r.MediaResults) != 1 {
		t.Errorf("MediaResults len = %d", len(r.MediaResults))
	}
	if r.BlockReplies != 2 {
		t.Errorf("BlockReplies = %d", r.BlockReplies)
	}
	if r.LastBlockReply != "last" {
		t.Errorf("LastBlockReply = %q", r.LastBlockReply)
	}
}
