package pipeline

import (
	"context"
	"fmt"
	"log/slog"
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
// Setup: [ContextStage]. Iteration: [ThinkStage, PruneStage, ToolStage, ObserveStage, CheckpointStage].
// Finalize: [FinalizeStage].
func NewDefaultPipeline(deps PipelineDeps) *Pipeline {
	d := &deps
	memFlush := NewMemoryFlushStage(d)

	setup := []Stage{
		NewContextStage(d),
	}
	iteration := []Stage{
		NewThinkStage(d),
		NewPruneStage(d, memFlush),
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

	// 2. Iteration loop
	for state.Iteration = 0; state.Iteration < p.Deps.Config.MaxIterations; state.Iteration++ {
		for _, stage := range p.iteration {
			if err := stage.Execute(ctx, state); err != nil {
				return nil, fmt.Errorf("iter %d %s: %w", state.Iteration, stage.Name(), err)
			}

			if swr, ok := stage.(StageWithResult); ok {
				switch swr.Result() {
				case BreakLoop:
					state.ExitCode = BreakLoop
					goto finalize
				case AbortRun:
					state.ExitCode = AbortRun
					goto finalize
				}
			}
		}

		if ctx.Err() != nil {
			state.ExitCode = AbortRun
			break
		}
	}

finalize:
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
	return result, nil
}
