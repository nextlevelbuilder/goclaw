package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// TestPipelineRunSkipsSetupWhenResuming proves a checkpoint-restored state skips
// the setup stages entirely and starts the iteration loop at the restored
// iteration instead of 0.
func TestPipelineRunSkipsSetupWhenResuming(t *testing.T) {
	setup := newMockStageNoResult("setup")
	iter := newMockStageNoResult("iter")

	p := NewPipeline(
		[]Stage{setup},
		[]Stage{iter},
		nil,
		PipelineDeps{Config: PipelineConfig{MaxIterations: 10}},
	)

	state := buildMinimalRunState()
	// Simulate a restored state: Iteration already past, resuming true.
	state.Iteration = 6
	state.resuming = true

	_, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if setup.execCnt != 0 {
		t.Fatalf("setup execCnt = %d, want 0 (resume skips setup)", setup.execCnt)
	}
	// Loop runs from iteration 6..9 (4 iterations), not 0..9.
	if iter.execCnt != 4 {
		t.Fatalf("iter execCnt = %d, want 4 (iterations 6,7,8,9)", iter.execCnt)
	}
	if state.Iteration != 10 {
		t.Fatalf("state.Iteration = %d, want 10 (loop exhausted)", state.Iteration)
	}
}

// TestPipelineRunSetupRunsForFreshState proves non-resuming states still run the
// full setup + loop from iteration 0 (regression guard).
func TestPipelineRunSetupRunsForFreshState(t *testing.T) {
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
		t.Fatalf("setup execCnt = %d, want 1", setup.execCnt)
	}
	if iter.execCnt != 3 {
		t.Fatalf("iter execCnt = %d, want 3", iter.execCnt)
	}
	if state.Iteration != 3 {
		t.Fatalf("state.Iteration = %d, want 3", state.Iteration)
	}
}

// TestContextStageResumeOnlySetsCtx proves a resuming state bypasses all
// message/workspace/context rebuilding and only keeps state.Ctx flowing.
func TestContextStageResumeOnlySetsCtx(t *testing.T) {
	deps := &PipelineDeps{
		BuildMessages: func(ctx context.Context, input *RunInput, history []providers.Message, summary string) ([]providers.Message, error) {
			t.Fatal("BuildMessages must not run on resume")
			return nil, nil
		},
		LoadSessionHistory: func(ctx context.Context, sessionKey string) ([]providers.Message, string) {
			t.Fatal("LoadSessionHistory must not run on resume")
			return nil, ""
		},
		ResolveWorkspace: func(ctx context.Context, input *RunInput) (*workspace.WorkspaceContext, error) {
			t.Fatal("ResolveWorkspace must not run on resume")
			return nil, nil
		},
		InjectContext: func(ctx context.Context, input *RunInput) (context.Context, error) {
			t.Fatal("InjectContext must not run on resume")
			return ctx, nil
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 1},
	}
	stage := NewContextStage(deps)

	state := defaultState()
	state.resuming = true
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "preserved"}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if state.Ctx == nil {
		t.Fatal("state.Ctx must be set on resume")
	}
	// Restored messages must survive the resume.
	if len(state.Messages.History()) != 1 || state.Messages.History()[0].Content != "preserved" {
		t.Fatalf("history clobbered on resume: %+v", state.Messages.History())
	}
}
