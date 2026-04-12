package agent

import (
	"fmt"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestMaybePromoteRecoverableLoopKillToCloseout_ReadOnly(t *testing.T) {
	state := pipeline.NewRunState(&pipeline.RunInput{SessionKey: "session-1"}, nil, "", nil)
	state.Turn.RecordToolObservation("web_search", "Search results kept repeating.", false)
	bridgeRS := &runState{
		loopKilled:   true,
		finalContent: "CRITICAL: 36 consecutive read-only tool calls (32 unique files). Stopping — write your findings before reading more.",
	}

	msg := maybePromoteRecoverableLoopKillToCloseout(state, bridgeRS, "web_search")
	if msg == nil {
		t.Fatal("expected closeout message")
	}
	if !state.Turn.ForceAnswerOnly {
		t.Fatal("expected closeout mode to be armed")
	}
	if state.Turn.CloseoutReason != pipeline.TurnCloseoutReasonReadOnlyBudgetExhausted {
		t.Fatalf("CloseoutReason = %q", state.Turn.CloseoutReason)
	}
	if bridgeRS.loopKilled {
		t.Fatal("expected loop kill to be converted into closeout mode")
	}
	if state.Tool.LoopKilled {
		t.Fatal("expected pipeline LoopKilled flag to be cleared")
	}
	if msg.Role != "system" {
		t.Fatalf("message role = %q, want system", msg.Role)
	}
}

func TestMaybePromoteRecoverableLoopKillToCloseout_ExecStaysDirect(t *testing.T) {
	state := pipeline.NewRunState(&pipeline.RunInput{SessionKey: "session-1"}, nil, "", nil)
	bridgeRS := &runState{
		loopKilled:   true,
		finalContent: "I already ran `docker info` and got this result:\n\n```text\nsh: docker: not found\n```",
	}

	msg := maybePromoteRecoverableLoopKillToCloseout(state, bridgeRS, "exec")
	if msg != nil {
		t.Fatalf("expected nil message, got %+v", msg)
	}
	if state.Turn.ForceAnswerOnly {
		t.Fatal("exec recovery should keep the direct observed result path")
	}
	if !bridgeRS.loopKilled {
		t.Fatal("expected loopKilled to remain true for exec direct recovery")
	}
}

func TestApplyTurnCloseoutToolPolicy_StripsToolsAndInjectsOnce(t *testing.T) {
	state := pipeline.NewRunState(&pipeline.RunInput{SessionKey: "session-1"}, nil, "", nil)
	state.Turn.ArmCloseout(pipeline.TurnCloseoutReasonToolBudgetExhausted)
	defs := []providers.ToolDefinition{{
		Type: "function",
		Function: providers.ToolFunctionSchema{
			Name:        "web_search",
			Description: "Search the web",
			Parameters:  map[string]any{"type": "object"},
		},
	}}

	filtered, msg := applyTurnCloseoutToolPolicy(state, defs)
	if len(filtered) != 0 {
		t.Fatalf("expected tools stripped, got %d defs", len(filtered))
	}
	if msg == nil {
		t.Fatal("expected closeout instruction")
	}

	filtered, msg = applyTurnCloseoutToolPolicy(state, defs)
	if len(filtered) != 0 {
		t.Fatalf("expected tools stripped on second call, got %d defs", len(filtered))
	}
	if msg != nil {
		t.Fatalf("expected instruction only once, got %+v", msg)
	}
}

func TestMakeCheckReadOnly_CriticalPromotesToCloseout(t *testing.T) {
	loop := &Loop{id: "tester"}
	req := &RunRequest{RunID: "run-1"}
	bridgeRS := &runState{}
	state := pipeline.NewRunState(&pipeline.RunInput{SessionKey: "session-1"}, nil, "", nil)
	state.Turn.RecordToolObservation("web_search", "Result 1", false)

	for i := 0; i < readOnlyExplorationCritical; i++ {
		bridgeRS.loopDetector.recordMutation("web_search", map[string]any{
			"query": fmt.Sprintf("query-%d", i),
		})
	}

	check := loop.makeCheckReadOnly(req, bridgeRS)
	msg, shouldBreak := check(state)

	if shouldBreak {
		t.Fatal("expected closeout promotion instead of hard break")
	}
	if msg == nil {
		t.Fatal("expected closeout system message")
	}
	if !state.Turn.ForceAnswerOnly {
		t.Fatal("expected closeout mode to be armed")
	}
	if state.Tool.LoopKilled {
		t.Fatal("expected LoopKilled=false after promotion")
	}
}
