package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// reasonedAborter aborts the run and records a diagnosable cause, the way
// PruneStage does when compaction cannot bring history under budget.
type reasonedAborter struct {
	*mockStage
	reason  error
	content string
}

func (s *reasonedAborter) Execute(ctx context.Context, state *RunState) error {
	s.execCnt++
	if s.content != "" {
		state.Observe.FinalContent = s.content
	}
	state.AbortReason = s.reason
	return nil
}

func (s *reasonedAborter) Result() StageResult { return AbortRun }

// A run that aborts before ever reaching the provider must surface an error.
// Returning (result, nil) made it indistinguishable from a provider that
// answered with nothing, so callers settled work on a blank deliverable.
func TestPipelineRunReportsAbortReasonWhenNothingProduced(t *testing.T) {
	t.Parallel()
	reason := errors.New("history over budget after compaction: tokens=213389 budget=150517")
	aborter := &reasonedAborter{mockStage: &mockStage{name: "prune"}, reason: reason}

	p := NewPipeline(nil, []Stage{aborter}, nil, PipelineDeps{Config: PipelineConfig{MaxIterations: 5}})

	result, err := p.Run(context.Background(), buildMinimalRunState())
	if err == nil {
		t.Fatal("Run() error = nil, want the abort reason surfaced")
	}
	if !errors.Is(err, reason) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, reason)
	}
	if !strings.Contains(err.Error(), "213389") {
		t.Errorf("error %q lost the diagnosable detail", err.Error())
	}
	if result == nil {
		t.Fatal("Run() result = nil, want the partial result alongside the error")
	}
}

// An abort AFTER the run produced something is not a failure to report: the
// content is real and must still be delivered.
func TestPipelineRunKeepsPartialContentOnAbort(t *testing.T) {
	t.Parallel()
	aborter := &reasonedAborter{
		mockStage: &mockStage{name: "prune"},
		reason:    errors.New("history over budget after compaction"),
		content:   "here is the answer I already produced",
	}

	p := NewPipeline(nil, []Stage{aborter}, nil, PipelineDeps{Config: PipelineConfig{MaxIterations: 5}})

	result, err := p.Run(context.Background(), buildMinimalRunState())
	if err != nil {
		t.Fatalf("Run() error = %v, want nil when the run produced content", err)
	}
	if result.Content != "here is the answer I already produced" {
		t.Errorf("result.Content = %q, want the partial answer preserved", result.Content)
	}
}

// An abort with no recorded cause (ctx cancel, truncation retries exhausted)
// keeps the pre-existing contract: no synthetic error.
func TestPipelineRunStaysSilentOnAbortWithoutReason(t *testing.T) {
	t.Parallel()
	aborter := newMockStageWithResult("aborter", AbortRun)

	p := NewPipeline(nil, []Stage{aborter}, nil, PipelineDeps{Config: PipelineConfig{MaxIterations: 5}})

	if _, err := p.Run(context.Background(), buildMinimalRunState()); err != nil {
		t.Fatalf("Run() error = %v, want nil for an abort with no reason", err)
	}
}
