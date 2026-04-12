package agent

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func noteTurnObservation(state *pipeline.RunState, toolName string, result *tools.Result) {
	if state == nil || result == nil {
		return
	}
	state.Turn.RecordToolObservation(toolName, result.ForLLM, result.IsError)
}

func applyTurnCloseoutToolPolicy(
	state *pipeline.RunState,
	toolDefs []providers.ToolDefinition,
) ([]providers.ToolDefinition, *providers.Message) {
	if state == nil || !state.Turn.ForceAnswerOnly {
		return toolDefs, nil
	}
	if state.Turn.CloseoutHintInjected {
		return nil, nil
	}
	state.Turn.CloseoutHintInjected = true
	return nil, &providers.Message{
		Role:    "system",
		Content: state.Turn.CloseoutInstruction(),
	}
}

func maybePromoteRecoverableLoopKillToCloseout(
	state *pipeline.RunState,
	bridgeRS *runState,
	toolName string,
) *providers.Message {
	if state == nil || bridgeRS == nil || !bridgeRS.loopKilled {
		return nil
	}
	if toolName == "exec" || toolName == "bash" {
		return nil
	}

	reason, ok := classifyRecoverableLoopKill(bridgeRS.finalContent)
	if !ok {
		return nil
	}

	state.Turn.ArmCloseout(reason)
	state.Turn.CloseoutHintInjected = true

	bridgeRS.loopKilled = false
	bridgeRS.finalContent = ""
	state.Tool.LoopKilled = false
	state.Observe.FinalContent = ""

	return &providers.Message{
		Role:    "system",
		Content: state.Turn.CloseoutInstruction(),
	}
}

func classifyRecoverableLoopKill(content string) (pipeline.TurnCloseoutReason, bool) {
	lower := strings.ToLower(strings.TrimSpace(content))
	switch {
	case strings.Contains(lower, "read-only tool calls"):
		return pipeline.TurnCloseoutReasonReadOnlyBudgetExhausted, true
	case strings.Contains(lower, "without making progress"),
		strings.Contains(lower, "same result"),
		strings.Contains(lower, "runaway loop"):
		return pipeline.TurnCloseoutReasonNoProgressLoop, true
	default:
		return "", false
	}
}
