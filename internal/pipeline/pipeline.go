package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Pipeline orchestrates stage execution for a single agent run.
type Pipeline struct {
	setup     []Stage // runs once before iteration loop
	iteration []Stage // runs per iteration
	finalize  []Stage // runs once after loop

	Deps PipelineDeps
}

// NewPipeline creates a pipeline from explicit stage lists.
func NewPipeline(setup, iteration, finalize []Stage, deps PipelineDeps) *Pipeline {
	return &Pipeline{
		setup:     setup,
		iteration: iteration,
		finalize:  finalize,
		Deps:      deps,
	}
}

// NewDefaultPipeline creates the standard 8-stage pipeline.
// Setup: [ContextStage]. Iteration: [PruneStage, ThinkStage, ToolStage, ObserveStage, CheckpointStage].
// Finalize: [FinalizeStage].
func NewDefaultPipeline(deps PipelineDeps) *Pipeline {
	d := &deps
	memFlush := NewMemoryFlushStage(d)

	setup := []Stage{
		NewContextStage(d),
	}
	iteration := []Stage{
		NewPruneStage(d, memFlush),
		NewThinkStage(d),
		NewToolStage(d),
		NewObserveStage(d),
		NewCheckpointStage(d),
	}
	finalize := []Stage{
		NewFinalizeStage(d),
	}
	return NewPipeline(setup, iteration, finalize, deps)
}

// Run executes the full pipeline for a single agent run.
func (p *Pipeline) Run(ctx context.Context, state *RunState) (*RunResult, error) {
	start := time.Now()

	// 1. Setup (once)
	for _, stage := range p.setup {
		if err := stage.Execute(ctx, state); err != nil {
			return nil, fmt.Errorf("setup %s: %w", stage.Name(), err)
		}
	}
	// Propagate enriched context from setup stages (ContextStage injects agent/user/workspace values).
	if state.Ctx != nil {
		ctx = state.Ctx
	}

	// 2. Iteration loop
	// BreakLoop: complete all remaining stages in this iteration (ObserveStage must
	// capture FinalContent), then exit the outer loop.
	// AbortRun: exit inner loop immediately (unrecoverable, e.g. over budget after compaction).
	for state.Iteration = 0; state.Iteration < p.Deps.Config.MaxIterations; state.Iteration++ {
		for _, stage := range p.iteration {
			// ThinkStage may replace state.Ctx (for example when Team Work
			// enforcement degrades to self-execution). Every later stage must
			// observe the current run context, not the setup-time snapshot.
			if state.Ctx != nil {
				ctx = state.Ctx
			}
			if err := stage.Execute(ctx, state); err != nil {
				return nil, fmt.Errorf("iter %d %s: %w", state.Iteration, stage.Name(), err)
			}
			// AbortRun exits inner loop immediately — skip remaining stages.
			if swr, ok := stage.(StageWithResult); ok && swr.Result() == AbortRun {
				state.ExitCode = AbortRun
				break
			}
		}

		// Check exit after all stages (or after AbortRun broke early).
		if state.ExitCode == AbortRun {
			break
		}
		for _, stage := range p.iteration {
			if swr, ok := stage.(StageWithResult); ok && swr.Result() == BreakLoop {
				state.ExitCode = BreakLoop
				break
			}
		}
		if state.ExitCode == BreakLoop {
			if state.Observe.ContinueAfterFinal {
				state.Observe.ContinueAfterFinal = false
				state.ExitCode = Continue
				continue
			}
			break
		}
		if ctx.Err() != nil {
			state.ExitCode = AbortRun
			break
		}
	}

	// 3. Finalize (once, errors logged not fatal).
	// Use background context so finalize stages can persist state even after cancellation.
	finalizeCtx := context.WithoutCancel(ctx)
	for _, stage := range p.finalize {
		if err := stage.Execute(finalizeCtx, state); err != nil {
			slog.Warn("finalize stage error", "stage", stage.Name(), "err", err)
		}
	}

	result := state.BuildResult()
	result.Duration = time.Since(start)
	if result.Duration <= 0 {
		result.Duration = time.Nanosecond
	}
	// An abort with a diagnosable cause is a failed run, not an empty answer.
	// Returning (result, nil) here made a run that never reached the provider
	// look identical to one whose provider returned nothing: callers settled
	// tasks on a blank deliverable and the real cause (compaction timing out,
	// so history never fit the window) never surfaced. Only report the error
	// when the run genuinely produced nothing — an abort after partial work
	// still has content worth delivering.
	if state.ExitCode == AbortRun && state.AbortReason != nil && runProducedNothing(result) {
		return result, state.AbortReason
	}
	return result, nil
}

// runProducedNothing reports whether a run yielded no deliverable output at all:
// no content, no thinking, no images, no deliverables and no tool calls.
func runProducedNothing(result *RunResult) bool {
	if result == nil {
		return true
	}
	return strings.TrimSpace(result.Content) == "" &&
		strings.TrimSpace(result.Thinking) == "" &&
		len(result.Deliverables) == 0 &&
		len(result.MediaResults) == 0 &&
		result.ToolCalls == 0
}
