package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- Weak-model chaos through the REAL pipeline loop ---
//
// These tests drive the full NewDefaultPipeline().Run() loop through
// weak-model failure scenarios: malformed tool calls, empty replies,
// premature completion, repeated tool loops, and invalid tool JSON. Each
// scenario asserts that the pipeline's recovery path fired and that the run
// COMPLETED (ExitCode != AbortRun) instead of failing.
//
// The tool-loop detector lives in the agent package (internal/agent/toolloop.go,
// type toolLoopState) and is bridged into the pipeline via state.Tool.LoopKilled
// (loop_pipeline_tool_callbacks.go syncBridgeToState). A pure pipeline test
// cannot attach the agent's detector (import cycle), so the fake ExecuteToolCall
// below simulates the critical signal after N identical calls — which is exactly
// what ToolStage consumes to force a BreakLoop.
//
// Note on the truncation/parse-error repair counter: ThinkStage resets
// state.Think.TruncRetries to 0 on a successful response (think_stage.go), so
// the tests capture the counter INSIDE the CallLLM closure (at the moment the
// repaired call is made) rather than after Run() returns.

// TestWeakModel_MalformedToolCall_RepairsAndContinues verifies the malformed
// tool-call recovery: the model emits a tool call whose arguments were
// truncated (finish_reason="tool_calls" with empty args on read_file), the
// ThinkStage truncation-repair path fires, and the loop continues to a
// successful final answer instead of aborting.
func TestWeakModel_MalformedToolCall_RepairsAndContinues(t *testing.T) {
	t.Parallel()

	var callCount int
	var truncRetriesAtRepair int
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 8,
			MaxTokens:     1000,
		},
		CallLLM: func(_ context.Context, state *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				// Gemini-style truncation: finish_reason="tool_calls" with empty
				// arguments on a mutating tool — the weak-model malformed call.
				return &providers.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls: []providers.ToolCall{{
						ID:   "malformed-1",
						Name: "read_file",
						// Arguments nil/empty -> toolCallsHaveMissingRequiredArgs.
					}},
				}, nil
			}
			// The repair happened on the previous iteration; capture the counter
			// before the success path resets it to 0.
			truncRetriesAtRepair = state.Think.TruncRetries
			return &providers.ChatResponse{
				Content:      "repaired and answered",
				FinishReason: "stop",
			}, nil
		},
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			// The pipeline still hands the malformed call to the executor in the
			// same iteration (LastResponse carries it). Real tools return an error
			// result; surface that as a tool message so the loop keeps pairing.
			return []providers.Message{{
				Role:       "tool",
				Content:    "error: missing required argument 'path'",
				ToolCallID: tc.ID,
				IsError:    true,
			}}, nil
		},
	}

	state := defaultState()
	result, err := NewDefaultPipeline(deps).Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if state.ExitCode == AbortRun {
		t.Fatal("run aborted; the truncation repair path should have continued the loop")
	}
	// The repair counter was 1 when the model was called again — repair fired.
	if truncRetriesAtRepair != 1 {
		t.Errorf("TruncRetries at repair = %d, want 1 (repair path must have fired)", truncRetriesAtRepair)
	}
	if !strings.Contains(result.Content, "repaired and answered") {
		t.Errorf("final content = %q, want the repaired final answer", result.Content)
	}
	if callCount != 2 {
		t.Errorf("LLM calls = %d, want 2 (malformed + repaired)", callCount)
	}
}

// TestWeakModel_EmptyOutput_Recovers verifies the bounded empty-reply nudge: the
// model's first response is empty (stop, no tools), ThinkStage nudges it once,
// and the second response carries content — the run completes with the content
// delivered, not the placeholder fallback.
func TestWeakModel_EmptyOutput_Recovers(t *testing.T) {
	t.Parallel()

	var callCount int
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 8,
			MaxTokens:     1000,
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				return &providers.ChatResponse{
					Content:      "",
					FinishReason: "stop",
				}, nil
			}
			return &providers.ChatResponse{
				Content:      "recovered answer",
				FinishReason: "stop",
			}, nil
		},
	}

	state := defaultState()
	result, err := NewDefaultPipeline(deps).Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if state.ExitCode == AbortRun {
		t.Fatal("run aborted; the empty-output recovery should have continued")
	}
	// The recovery counter: one empty reply consumed the nudge.
	if state.Think.EmptyReplyRetries != 1 {
		t.Errorf("EmptyReplyRetries = %d, want 1 (empty-output recovery fired)", state.Think.EmptyReplyRetries)
	}
	if !strings.Contains(result.Content, "recovered answer") {
		t.Errorf("final content = %q, want the recovered answer", result.Content)
	}
	if state.Observe.FinalContent == "" {
		t.Error("FinalContent empty; the run should have delivered the recovered answer")
	}
}

// TestWeakModel_PrematureCompletion_GateForcesContinue verifies the opt-in
// ContinuationGate: the model says "done" (empty final answer) three times
// while no tool work was done and no deliverable exists. ThinkStage's bounded
// nudge handles the first two empties; on the third the gate fires
// ContinueAfterFinal, the pipeline runs one more iteration, and the fourth
// answer carries the actual result.
//
// Timeline (MaxIterations=8): iter 0 empty -> nudge, iter 1 empty -> nudge +
// gate fires (one continuation), iter 2 empty -> BreakLoop consumed by
// ContinueAfterFinal, iter 3 content -> BreakLoop.
func TestWeakModel_PrematureCompletion_GateForcesContinue(t *testing.T) {
	// Mutates the process-wide reliability runtime — keep out of t.Parallel().
	enableContinuationGate(t)

	var callCount int
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 8,
			MaxTokens:     1000,
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			callCount++
			if callCount <= 3 {
				return &providers.ChatResponse{
					Content:      "",
					FinishReason: "stop",
				}, nil
			}
			return &providers.ChatResponse{
				Content:      "work completed after gate",
				FinishReason: "stop",
			}, nil
		},
	}

	state := defaultState()
	result, err := NewDefaultPipeline(deps).Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !state.Observe.ContinuationGateFired {
		t.Error("ContinuationGateFired = false, want true (gate must have forced a continuation)")
	}
	if state.ExitCode == AbortRun {
		t.Fatal("run aborted; the gate continuation should have completed")
	}
	if !strings.Contains(result.Content, "work completed after gate") {
		t.Errorf("final content = %q, want the post-gate answer", result.Content)
	}
	if state.Iteration != 3 {
		t.Errorf("Iteration = %d, want 3 (2 nudges + 1 gate continuation + exit)", state.Iteration)
	}
	if callCount != 4 {
		t.Errorf("LLM calls = %d, want 4 (3 empty + 1 gated answer)", callCount)
	}
}

// TestWeakModel_RepeatedToolLoop_DetectedNotInfinite verifies the loop is
// bounded: the model repeats the same tool call with identical arguments. The
// fake tool executor simulates the agent-side tool loop detector, which sets
// state.Tool.LoopKilled at the critical threshold (5 identical calls). ToolStage
// then forces BreakLoop — the run terminates long before MaxIterations instead
// of spinning.
func TestWeakModel_RepeatedToolLoop_DetectedNotInfinite(t *testing.T) {
	t.Parallel()

	var toolExecCount int
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 20,
			MaxTokens:     1000,
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{{
					ID:        "loop-1",
					Name:      "read_file",
					Arguments: map[string]any{"path": "/same/path.md"},
				}},
			}, nil
		},
		ExecuteToolCall: func(_ context.Context, state *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			toolExecCount++
			// The real detector lives in the agent package and cannot be imported
			// here (pipeline -> agent would be an import cycle). Its critical
			// threshold is 5 identical calls; simulate exactly that signal.
			if toolExecCount >= 5 {
				state.Tool.LoopKilled = true
			}
			return []providers.Message{{
				Role:       "tool",
				Content:    "result: " + tc.Name,
				ToolCallID: tc.ID,
			}}, nil
		},
	}

	state := defaultState()
	result, err := NewDefaultPipeline(deps).Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !state.Tool.LoopKilled {
		t.Error("LoopKilled = false, want true (loop detector must have tripped)")
	}
	if !result.LoopKilled {
		t.Error("result.LoopKilled = false, want true")
	}
	if state.ExitCode == AbortRun {
		t.Fatal("run aborted; the loop-kill path breaks the loop, it does not abort the run")
	}
	// 5 identical calls then BreakLoop: iterations 0-4 ran, so Iteration == 4,
	// well below MaxIterations — the loop was bounded, not infinite.
	if result.Iterations != 4 {
		t.Errorf("Iterations = %d, want 4 (5 tool calls then break)", result.Iterations)
	}
	if state.Tool.TotalToolCalls != 5 {
		t.Errorf("TotalToolCalls = %d, want 5", state.Tool.TotalToolCalls)
	}
	if state.Iteration >= 20 {
		t.Fatalf("run hit MaxIterations (%d); the loop was not bounded", state.Iteration)
	}
}

// TestWeakModel_InvalidJSON_RepairThenContinue verifies the invalid-JSON repair
// path: the first response carries a tool call with a truncated JSON envelope
// (ParseError set on a non-mutating tool so the parse-error branch fires).
// ThinkStage detects the parse error, increments its repair counter, and
// continues; the second response emits a valid tool call that the executor
// runs; the third response delivers the final answer.
func TestWeakModel_InvalidJSON_RepairThenContinue(t *testing.T) {
	t.Parallel()

	var callCount int
	var truncRetriesAtRepair int
	executed := map[string]int{}
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 8,
			MaxTokens:     1000,
		},
		CallLLM: func(_ context.Context, state *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			callCount++
			switch callCount {
			case 1:
				// Broken envelope: the provider could not parse the arguments JSON.
				// skill_search is not in the mutating allowlist, so this fires the
				// parse-error branch (not the missing-args truncation branch).
				return &providers.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls: []providers.ToolCall{{
						ID:         "broken-1",
						Name:       "skill_search",
						ParseError: "malformed JSON (24 chars): unexpected end of JSON input",
					}},
				}, nil
			case 2:
				// The repaired (valid) call after the retry hint. Capture the repair
				// counter before the success path resets it to 0.
				truncRetriesAtRepair = state.Think.TruncRetries
				return &providers.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls: []providers.ToolCall{{
						ID:        "valid-2",
						Name:      "read_file",
						Arguments: map[string]any{"path": "/notes.md"},
					}},
				}, nil
			default:
				return &providers.ChatResponse{
					Content:      "answered after repair",
					FinishReason: "stop",
				}, nil
			}
		},
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			executed[tc.Name]++
			return []providers.Message{{
				Role:       "tool",
				Content:    "result: " + tc.Name,
				ToolCallID: tc.ID,
			}}, nil
		},
	}

	state := defaultState()
	result, err := NewDefaultPipeline(deps).Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if state.ExitCode == AbortRun {
		t.Fatal("run aborted; the invalid-JSON repair should have continued the loop")
	}
	// The pipeline-level repair counter was 1 when the valid call was made.
	if truncRetriesAtRepair != 1 {
		t.Errorf("TruncRetries at repair = %d, want 1 (invalid-JSON repair must have fired)", truncRetriesAtRepair)
	}
	// The broken call still flows through the executor in iteration 0 (the real
	// agent normalizes it there); the valid call is the one we care about.
	if executed["read_file"] != 1 {
		t.Errorf("valid read_file executed %d times, want 1", executed["read_file"])
	}
	if executed["skill_search"] != 1 {
		t.Errorf("broken skill_search executed %d times, want 1 (iteration-0 dispatch)", executed["skill_search"])
	}
	if state.Tool.TotalToolCalls != 2 {
		t.Errorf("TotalToolCalls = %d, want 2 (broken + repaired calls)", state.Tool.TotalToolCalls)
	}
	if !strings.Contains(result.Content, "answered after repair") {
		t.Errorf("final content = %q, want the post-repair answer", result.Content)
	}
}
