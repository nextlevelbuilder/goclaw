package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// makeExecuteToolCall wraps tool execution: name resolution, execute, process result.
// Uses bridgeRS to share loop detection state between the pipeline and agent's processToolResult.
func (l *Loop) makeExecuteToolCall(req *RunRequest, bridgeRS *runState) func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall) ([]providers.Message, error) {
	emitRun := makeToolEmitRun(l, req)
	return func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall) ([]providers.Message, error) {
		registryName := l.resolveToolCallName(tc.Name)
		argsJSON, _ := json.Marshal(tc.Arguments)
		slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON))

		emitRun(AgentEvent{
			Type:    protocol.AgentEventToolCall,
			AgentID: l.id,
			RunID:   state.RunID,
			Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": tc.Arguments},
		})

		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)

		toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, result, state.Context.HadBootstrap)
		syncBridgeToState(bridgeRS, state, action)

		var msgs []providers.Message
		msgs = append(msgs, toolMsg)
		msgs = append(msgs, warningMsgs...)
		return msgs, nil
	}
}

// makeExecuteToolRaw wraps tool I/O only (parallel-safe, no state mutation).
// Returns tool message + *tools.Result as opaque raw data for ProcessToolResult.
func (l *Loop) makeExecuteToolRaw(req *RunRequest) func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
	return func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
		registryName := l.resolveToolCallName(tc.Name)
		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
		msg := providers.Message{
			Role:       "tool",
			Content:    result.ForLLM,
			ToolCallID: tc.ID,
			IsError:    result.IsError,
		}
		return msg, result, nil
	}
}

// makeProcessToolResult wraps post-execution bookkeeping (sequential, mutates bridgeRS).
// rawData is *tools.Result from ExecuteToolRaw — no re-execution.
func (l *Loop) makeProcessToolResult(req *RunRequest, bridgeRS *runState) func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
	emitRun := makeToolEmitRun(l, req)
	return func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
		registryName := l.resolveToolCallName(tc.Name)
		result, _ := rawData.(*tools.Result)
		if result == nil {
			return []providers.Message{rawMsg}
		}
		toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, result, state.Context.HadBootstrap)
		syncBridgeToState(bridgeRS, state, action)

		var msgs []providers.Message
		msgs = append(msgs, toolMsg)
		msgs = append(msgs, warningMsgs...)
		return msgs
	}
}

// makeCheckReadOnly wraps read-only streak detection using the bridged runState.
func (l *Loop) makeCheckReadOnly(req *RunRequest, bridgeRS *runState) func(state *pipeline.RunState) (*providers.Message, bool) {
	return func(state *pipeline.RunState) (*providers.Message, bool) {
		warnMsg, shouldBreak := l.checkReadOnlyStreak(bridgeRS, req)
		if shouldBreak {
			state.Tool.LoopKilled = bridgeRS.loopKilled
			state.Observe.FinalContent = bridgeRS.finalContent
		}
		return warnMsg, shouldBreak
	}
}

// syncBridgeToState copies side effects from bridgeRS to pipeline RunState.
func syncBridgeToState(bridgeRS *runState, state *pipeline.RunState, action toolResultAction) {
	state.Tool.LoopKilled = bridgeRS.loopKilled
	state.Tool.AsyncToolCalls = bridgeRS.asyncToolCalls
	state.Tool.Deliverables = bridgeRS.deliverables
	state.Evolution.BootstrapWrite = bridgeRS.bootstrapWriteDetected
	state.Evolution.TeamTaskSpawns = bridgeRS.teamTaskSpawns
	if state.Tool.LoopKilled && action == toolResultBreak {
		state.Observe.FinalContent = bridgeRS.finalContent
	}
}

// makeToolEmitRun creates a tool event emitter with request context.
func makeToolEmitRun(l *Loop, req *RunRequest) func(AgentEvent) {
	return func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.SessionKey = req.SessionKey
		event.UserID = req.UserID
		event.Channel = req.Channel
		l.emit(event)
	}
}
