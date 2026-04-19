package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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

		// Emit tool span start for tracing.
		toolStart := time.Now().UTC()
		toolSpanID := l.emitToolSpanStart(ctx, toolStart, tc.Name, tc.ID, string(argsJSON))

		// Inject agent audio snapshot so TTS tool (and any future audio consumers)
		// can read agent-level voice/model config without an extra DB lookup.
		if l.agentUUID != uuid.Nil {
			ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
				AgentID:     l.agentUUID,
				OtherConfig: append([]byte(nil), l.agentOtherConfig...), // defensive copy at dispatch
			})
		}

		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
		toolDuration := time.Since(toolStart)

		l.emitToolSpanEnd(ctx, toolSpanID, toolStart, result)

		// v3 evolution metrics: record tool execution non-blocking (best-effort).
		l.recordToolMetric(ctx, req.SessionKey, registryName, !result.IsError, toolDuration)

		toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, result, state.Context.HadBootstrap)
		syncBridgeToState(bridgeRS, state, action)

		var msgs []providers.Message
		msgs = append(msgs, toolMsg)
		msgs = append(msgs, warningMsgs...)
		return msgs, nil
	}
}

// toolRawResult wraps a tools.Result with timing for metrics recording.
type toolRawResult struct {
	result   *tools.Result
	duration time.Duration
}

// makeExecuteToolRaw wraps tool I/O only (parallel-safe, no state mutation).
// Returns tool message + toolRawResult (with timing + spanID) as opaque raw data for ProcessToolResult.
func (l *Loop) makeExecuteToolRaw(req *RunRequest) func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
	emitRun := makeToolEmitRun(l, req)
	return func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
		registryName := l.resolveToolCallName(tc.Name)
		argsJSON, _ := json.Marshal(tc.Arguments)
		slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON))

		// Emit tool.call event at I/O start — parity with sequential path (makeExecuteToolCall).
		// Without this, parallel tool execution (2+ concurrent tools) never notifies UI of
		// tool invocation, so `tool.result` arrives with no matching `tool.call` to update.
		// Bus.Broadcast is RWMutex-guarded; safe to call from parallel goroutines.
		emitRun(AgentEvent{
			Type:    protocol.AgentEventToolCall,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": tc.Arguments},
		})

		// Emit tool span start (goroutine-safe: channel send only).
		start := time.Now().UTC()
		spanID := l.emitToolSpanStart(ctx, start, tc.Name, tc.ID, string(argsJSON))

		// Inject agent audio snapshot (parallel path — same as sequential makeExecuteToolCall).
		if l.agentUUID != uuid.Nil {
			ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
				AgentID:     l.agentUUID,
				OtherConfig: append([]byte(nil), l.agentOtherConfig...), // defensive copy at dispatch
			})
		}

		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
		dur := time.Since(start)

		// Emit tool span end inside goroutine to prevent orphaned spans on ctx cancellation.
		l.emitToolSpanEnd(ctx, spanID, start, result)

		msg := providers.Message{
			Role:       "tool",
			Content:    result.ForLLM,
			ToolCallID: tc.ID,
			IsError:    result.IsError,
		}
		return msg, &toolRawResult{result: result, duration: dur}, nil
	}
}

// makeProcessToolResult wraps post-execution bookkeeping (sequential, mutates bridgeRS).
// rawData is *toolRawResult from ExecuteToolRaw — no re-execution.
func (l *Loop) makeProcessToolResult(req *RunRequest, bridgeRS *runState) func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
	emitRun := makeToolEmitRun(l, req)
	return func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
		registryName := l.resolveToolCallName(tc.Name)

		// Extract result and timing from toolRawResult wrapper.
		var result *tools.Result
		var dur time.Duration
		if raw, ok := rawData.(*toolRawResult); ok && raw != nil {
			result = raw.result
			dur = raw.duration
		} else if r, ok := rawData.(*tools.Result); ok {
			result = r // backward compat
		}
		if result == nil {
			return []providers.Message{rawMsg}
		}

		// Record tool metrics (non-blocking, best-effort).
		l.recordToolMetric(ctx, req.SessionKey, registryName, !result.IsError, dur)

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
	state.Evolution.TeamTaskCreates = bridgeRS.teamTaskCreates
	// Sync media results from v2 processToolResult → v3 pipeline state.
	// Without this, MEDIA: paths from tool results never reach FinalizeStage.
	if len(bridgeRS.mediaResults) > 0 {
		state.Tool.MediaResults = state.Tool.MediaResults[:0]
		for _, mr := range bridgeRS.mediaResults {
			state.Tool.MediaResults = append(state.Tool.MediaResults, pipeline.MediaResult{
				Path:        mr.Path,
				ContentType: mr.ContentType,
				Size:        mr.Size,
				AsVoice:     mr.AsVoice,
			})
		}
	}
	if state.Tool.LoopKilled && action == toolResultBreak {
		state.Observe.FinalContent = bridgeRS.finalContent
	}
}

// recordToolMetric records a tool execution metric non-blocking (best-effort).
// No-op when evolution metrics store is not configured.
func (l *Loop) recordToolMetric(ctx context.Context, sessionKey, toolName string, success bool, duration time.Duration) {
	if l.evolutionMetricsStore == nil {
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		value, _ := json.Marshal(map[string]any{
			"success":     success,
			"duration_ms": duration.Milliseconds(),
		})
		if err := l.evolutionMetricsStore.RecordMetric(bgCtx, store.EvolutionMetric{
			ID:         uuid.New(),
			TenantID:   tenantID,
			AgentID:    l.agentUUID,
			SessionKey: sessionKey,
			MetricType: store.MetricTool,
			MetricKey:  toolName,
			Value:      value,
		}); err != nil {
			slog.Debug("evolution.metric.record_failed", "tool", toolName, "error", err)
		}
	}()
}

// makeToolEmitRun creates a tool event emitter with request context.
func makeToolEmitRun(l *Loop, req *RunRequest) func(AgentEvent) {
	return func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.SessionKey = req.SessionKey
		event.SenderID = req.SenderID
		event.UserID = req.UserID
		event.Channel = req.Channel
		l.emit(event)
	}
}
