package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestTurnState_ArmCloseoutBlockedWhenSignalsPresent(t *testing.T) {
	var ts TurnState
	ts.RecordToolObservation("exec", "command denied by safety policy: docker.sock", true)
	ts.ArmCloseout(TurnCloseoutReasonReadOnlyBudgetExhausted)

	if !ts.ForceAnswerOnly {
		t.Fatal("expected ForceAnswerOnly=true")
	}
	if ts.Phase != TurnPhaseBlocked {
		t.Fatalf("Phase = %q, want %q", ts.Phase, TurnPhaseBlocked)
	}
	if ts.CloseoutReason != TurnCloseoutReasonReadOnlyBudgetExhausted {
		t.Fatalf("CloseoutReason = %q", ts.CloseoutReason)
	}
	if !strings.Contains(ts.CloseoutInstruction(), "blocked by policy") {
		t.Fatalf("expected blocked-by-policy instruction, got %q", ts.CloseoutInstruction())
	}
}

func TestToolStage_MaxToolCalls_ArmsCloseoutInsteadOfBreak(t *testing.T) {
	stage := NewToolStage(&PipelineDeps{
		Config: PipelineConfig{MaxToolCalls: 3},
	})
	state := NewRunState(&RunInput{SessionKey: "session-1"}, nil, "", nil)
	state.Tool.TotalToolCalls = 3

	stage.checkExitConditions(state)

	if stage.Result() != Continue {
		t.Fatalf("Result() = %v, want Continue", stage.Result())
	}
	if !state.Turn.ForceAnswerOnly {
		t.Fatal("expected closeout mode to be armed")
	}
	if state.Turn.CloseoutReason != TurnCloseoutReasonToolBudgetExhausted {
		t.Fatalf("CloseoutReason = %q", state.Turn.CloseoutReason)
	}
	pending := state.Messages.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending count = %d, want 1", len(pending))
	}
	if pending[0].Role != "system" {
		t.Fatalf("pending role = %q, want system", pending[0].Role)
	}
}

func TestTurnState_ArmNeedsHuman(t *testing.T) {
	var ts TurnState
	ts.ArmNeedsHuman(TurnCloseoutReasonConstraintNeedsHuman)

	if ts.Phase != TurnPhaseNeedsHuman {
		t.Fatalf("Phase = %q, want %q", ts.Phase, TurnPhaseNeedsHuman)
	}
	if !ts.ForceAnswerOnly {
		t.Fatal("expected ForceAnswerOnly=true")
	}
	if !strings.Contains(ts.CloseoutInstruction(), "requires human action") {
		t.Fatalf("expected closeout instruction to mention human action, got %q", ts.CloseoutInstruction())
	}
}

func TestToolStage_ConstraintBlockArmsNeedsHuman(t *testing.T) {
	stage := NewToolStage(&PipelineDeps{
		Config: PipelineConfig{MaxToolCalls: 10},
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			t.Fatal("expected tool execution to be blocked pre-call")
			return nil, nil
		},
	})
	state := NewRunState(&RunInput{SessionKey: "session-1", RunID: "run-1"}, nil, "", nil)
	state.EnsureConstraintStore().Add(Constraint{
		Kind:       ConstraintBinaryMissing,
		Subject:    "git",
		Severity:   SeverityHard,
		Resolution: ResolutionHumanRequired,
		Message:    "git is not installed",
	})
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{
			ID:   "tc-1",
			Name: "exec",
			Arguments: map[string]any{
				"command": "git clone https://example.com/repo.git",
			},
		}},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.Turn.Phase != TurnPhaseNeedsHuman {
		t.Fatalf("Phase = %q, want %q", state.Turn.Phase, TurnPhaseNeedsHuman)
	}
	if !state.Turn.ForceAnswerOnly {
		t.Fatal("expected closeout mode to be armed")
	}
	if state.Tool.TotalToolCalls != 1 {
		t.Fatalf("TotalToolCalls = %d, want 1", state.Tool.TotalToolCalls)
	}
	if len(state.Messages.Pending()) == 0 {
		t.Fatal("expected blocked message to be appended")
	}
}

func TestToolStage_TwoConstraintBlocksArmPartialCloseout(t *testing.T) {
	stage := NewToolStage(&PipelineDeps{
		Config: PipelineConfig{MaxToolCalls: 10},
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			t.Fatal("expected tool execution to be blocked pre-call")
			return nil, nil
		},
	})
	state := NewRunState(&RunInput{SessionKey: "session-1", RunID: "run-1"}, nil, "", nil)
	execArgs := map[string]any{"command": "git clone https://example.com/repo.git"}
	state.EnsureConstraintStore().Add(Constraint{
		Kind:       ConstraintCapacityExhausted,
		Subject:    "spawn.children",
		Severity:   SeverityHard,
		Resolution: ResolutionSelfReroute,
		Message:    "child limit reached",
	})
	state.EnsureConstraintStore().Add(Constraint{
		Kind:       ConstraintRepeatedFailure,
		Subject:    ToolTargetKey("exec", execArgs),
		Severity:   SeverityHard,
		Resolution: ResolutionSelfReroute,
		Message:    "same exec target already failed repeatedly",
	})
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{
			{ID: "tc-1", Name: "spawn", Arguments: map[string]any{"task": "analyze"}},
			{ID: "tc-2", Name: "exec", Arguments: execArgs},
		},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.Turn.Phase != TurnPhasePartial {
		t.Fatalf("Phase = %q, want %q", state.Turn.Phase, TurnPhasePartial)
	}
	if !state.Turn.ForceAnswerOnly {
		t.Fatal("expected closeout mode to be armed")
	}
}

func TestFinalizeStage_ForcedCloseoutFallbackUsed(t *testing.T) {
	stage := NewFinalizeStage(&PipelineDeps{})
	state := NewRunState(&RunInput{SessionKey: "session-1"}, nil, "", nil)
	state.Turn.RecordToolObservation("web_search", "Search results kept looping without a decisive answer.", true)
	state.Turn.ArmCloseout(TurnCloseoutReasonNoProgressLoop)
	state.Observe.FinalContent = ""

	if err := stage.Execute(t.Context(), state); err != nil {
		t.Fatalf("FinalizeStage.Execute() error = %v", err)
	}
	if state.Observe.FinalContent == "" {
		t.Fatal("expected fallback content")
	}
	if !strings.Contains(state.Observe.FinalContent, "Status: Partial result.") {
		t.Fatalf("unexpected fallback content: %q", state.Observe.FinalContent)
	}
	if state.Turn.Phase != TurnPhasePartial {
		t.Fatalf("Phase = %q, want %q", state.Turn.Phase, TurnPhasePartial)
	}
}

func TestTurnState_ShouldUseCloseoutFallback(t *testing.T) {
	ts := TurnState{ForceAnswerOnly: true}
	cases := []struct {
		content string
		want    bool
	}{
		{"", true},
		{"...", true},
		{"CRITICAL: 36 consecutive read-only tool calls", true},
		{"I was unable to complete this task", true},
		{"Here is the best answer from gathered evidence.", false},
	}
	for _, tc := range cases {
		if got := ts.ShouldUseCloseoutFallback(tc.content); got != tc.want {
			t.Fatalf("ShouldUseCloseoutFallback(%q) = %v, want %v", tc.content, got, tc.want)
		}
	}
}

func TestFinalizeStage_ForcedCloseoutFallbackStillPersistsAssistantMessage(t *testing.T) {
	stage := NewFinalizeStage(&PipelineDeps{})
	state := NewRunState(&RunInput{SessionKey: "session-1"}, nil, "", nil)
	state.Turn.RecordToolObservation("web_search", "Latest evidence snippet", false)
	state.Turn.ArmCloseout(TurnCloseoutReasonReadOnlyBudgetExhausted)
	state.Observe.FinalContent = "CRITICAL: 36 consecutive read-only tool calls"

	if err := stage.Execute(t.Context(), state); err != nil {
		t.Fatalf("FinalizeStage.Execute() error = %v", err)
	}
	history := state.Messages.History()
	if len(history) != 1 {
		t.Fatalf("history count = %d, want 1", len(history))
	}
	if history[0].Role != "assistant" {
		t.Fatalf("assistant role = %q", history[0].Role)
	}
	if strings.HasPrefix(history[0].Content, "CRITICAL:") {
		t.Fatalf("expected guard text to be replaced, got %q", history[0].Content)
	}
}
