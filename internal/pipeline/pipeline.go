package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// Pipeline orchestrates stage execution for a single agent run.
type Pipeline struct {
	setup     []Stage // runs once before iteration loop
	iteration []Stage // runs per iteration
	finalize  []Stage // runs once after loop

	Config       PipelineConfig
	TokenCounter tokencount.TokenCounter
	EventBus     eventbus.DomainEventBus
}

// PipelineConfig holds pipeline-level settings.
type PipelineConfig struct {
	MaxIterations      int
	MaxToolCalls       int
	CheckpointInterval int // flush every N iterations (default 5)
	ContextWindow      int
	MaxTokens          int
}

// NewPipeline creates a pipeline from explicit stage lists.
func NewPipeline(setup, iteration, finalize []Stage, cfg PipelineConfig, tc tokencount.TokenCounter, eb eventbus.DomainEventBus) *Pipeline {
	return &Pipeline{
		setup:         setup,
		iteration:     iteration,
		finalize:      finalize,
		Config:       cfg,
		TokenCounter: tc,
		EventBus:     eb,
	}
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
	for state.Iteration = 0; state.Iteration < p.Config.MaxIterations; state.Iteration++ {
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
			break
		}
	}

finalize:
	// 3. Finalize (once, errors logged not fatal)
	for _, stage := range p.finalize {
		if err := stage.Execute(ctx, state); err != nil {
			slog.Warn("finalize stage error", "stage", stage.Name(), "err", err)
		}
	}

	return &RunResult{
		Content:        state.Observe.FinalContent,
		Thinking:       state.Observe.FinalThinking,
		TotalUsage:     state.Think.TotalUsage,
		Iterations:     state.Iteration,
		ToolCalls:      state.Tool.TotalToolCalls,
		LoopKilled:     state.Tool.LoopKilled,
		Duration:       time.Since(start),
		AsyncToolCalls: state.Tool.AsyncToolCalls,
	}, nil
}
