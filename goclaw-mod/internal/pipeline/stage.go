// Package pipeline provides a pluggable stage-based agent execution pipeline.
// All agent runs use this pipeline (v3 architecture).
//
// 8-stage loop: context → history → prompt → think → act → observe → memory → summarize.
package pipeline

import "context"

// StageResult signals how the pipeline should proceed after a stage.
type StageResult int

const (
	Continue  StageResult = iota // proceed to next stage
	BreakLoop                    // exit iteration loop (normal completion)
	AbortRun                     // abort entire run (error/kill)
)

// Stage is a single step in the agent pipeline.
// Stages are stateless — all mutable state lives in RunState.
type Stage interface {
	// Name returns a human-readable identifier for logging/tracing.
	Name() string

	// Execute performs the stage's work. Returns error to abort pipeline.
	Execute(ctx context.Context, state *RunState) error
}

// StageWithResult extends Stage to control pipeline flow.
// If a stage does not implement this, pipeline assumes Continue.
type StageWithResult interface {
	Stage
	Result() StageResult
}
