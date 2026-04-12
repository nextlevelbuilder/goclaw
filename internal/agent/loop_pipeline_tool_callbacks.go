package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
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

		if blocked := l.preToolHookBlock(ctx, tc); blocked != nil {
			applyToolResultTruncation(ctx, state, blocked)
			noteTurnObservation(state, registryName, blocked)
			constraintMsgs := l.applyRuntimeConstraints(state, tc, blocked)
			l.emitToolSpanEnd(ctx, toolSpanID, toolStart, blocked)
			toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, blocked, state.Context.HadBootstrap)
			warningMsgs = append(warningMsgs, constraintMsgs...)
			syncBridgeToState(bridgeRS, state, action)
			if closeoutMsg := maybePromoteRecoverableLoopKillToCloseout(state, bridgeRS, registryName); closeoutMsg != nil {
				warningMsgs = append(warningMsgs, *closeoutMsg)
			}
			if recoveryMsg := l.maybeArmSelfKnowledgeDirectAnswer(state, registryName, blocked); recoveryMsg != nil {
				warningMsgs = append(warningMsgs, *recoveryMsg)
			}
			var msgs []providers.Message
			msgs = append(msgs, toolMsg)
			msgs = append(msgs, warningMsgs...)
			return msgs, nil
		}

		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
		applyToolResultTruncation(ctx, state, result)
		toolDuration := time.Since(toolStart)
		noteTurnObservation(state, registryName, result)
		constraintMsgs := l.applyRuntimeConstraints(state, tc, result)

		l.emitToolSpanEnd(ctx, toolSpanID, toolStart, result)

		// v3 evolution metrics: record tool execution non-blocking (best-effort).
		l.recordToolMetric(ctx, req.SessionKey, registryName, !result.IsError, toolDuration)

		toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, result, state.Context.HadBootstrap)
		warningMsgs = append(warningMsgs, constraintMsgs...)
		syncBridgeToState(bridgeRS, state, action)
		if closeoutMsg := maybePromoteRecoverableLoopKillToCloseout(state, bridgeRS, registryName); closeoutMsg != nil {
			warningMsgs = append(warningMsgs, *closeoutMsg)
		}
		if recoveryMsg := l.maybeArmSelfKnowledgeDirectAnswer(state, registryName, result); recoveryMsg != nil {
			warningMsgs = append(warningMsgs, *recoveryMsg)
		}

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
	return func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
		registryName := l.resolveToolCallName(tc.Name)
		argsJSON, _ := json.Marshal(tc.Arguments)

		// Emit tool span start (goroutine-safe: channel send only).
		start := time.Now().UTC()
		spanID := l.emitToolSpanStart(ctx, start, tc.Name, tc.ID, string(argsJSON))

		if blocked := l.preToolHookBlock(ctx, tc); blocked != nil {
			applyToolResultTruncation(ctx, nil, blocked)
			l.applyRuntimeConstraints(nil, tc, blocked)
			l.emitToolSpanEnd(ctx, spanID, start, blocked)
			msg := providers.Message{
				Role:       "tool",
				Content:    blocked.ForLLM,
				ToolCallID: tc.ID,
				IsError:    blocked.IsError,
			}
			return msg, &toolRawResult{result: blocked, duration: time.Since(start)}, nil
		}

		result := l.tools.ExecuteWithContext(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
		applyToolResultTruncation(ctx, nil, result)
		l.applyRuntimeConstraints(nil, tc, result)
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
		noteTurnObservation(state, registryName, result)
		constraintMsgs := l.applyRuntimeConstraints(state, tc, result)

		// Record tool metrics (non-blocking, best-effort).
		l.recordToolMetric(ctx, req.SessionKey, registryName, !result.IsError, dur)

		toolMsg, warningMsgs, action := l.processToolResult(ctx, bridgeRS, req, emitRun, tc, registryName, result, state.Context.HadBootstrap)
		warningMsgs = append(warningMsgs, constraintMsgs...)
		syncBridgeToState(bridgeRS, state, action)
		if closeoutMsg := maybePromoteRecoverableLoopKillToCloseout(state, bridgeRS, registryName); closeoutMsg != nil {
			warningMsgs = append(warningMsgs, *closeoutMsg)
		}
		if recoveryMsg := l.maybeArmSelfKnowledgeDirectAnswer(state, registryName, result); recoveryMsg != nil {
			warningMsgs = append(warningMsgs, *recoveryMsg)
		}

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
			if closeoutMsg := maybePromoteRecoverableLoopKillToCloseout(state, bridgeRS, state.Turn.LastToolName); closeoutMsg != nil {
				return closeoutMsg, false
			}
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
		event.UserID = req.UserID
		event.Channel = req.Channel
		l.emit(event)
	}
}

func applyToolResultTruncation(ctx context.Context, state *pipeline.RunState, result *tools.Result) {
	if result == nil || result.ForLLM == "" {
		return
	}
	cfg := pipeline.DefaultTruncationConfig()
	switch {
	case tools.ToolWorkspaceFromCtx(ctx) != "":
		cfg = pipeline.TruncationConfigForWorkspace(tools.ToolWorkspaceFromCtx(ctx))
	case state != nil && state.Workspace != nil && state.Workspace.ActivePath != "":
		cfg = pipeline.TruncationConfigForWorkspace(state.Workspace.ActivePath)
	case workspace.FromContext(ctx) != nil && workspace.FromContext(ctx).ActivePath != "":
		cfg = pipeline.TruncationConfigForWorkspace(workspace.FromContext(ctx).ActivePath)
	}
	truncated, _ := pipeline.TruncateResult(result.ForLLM, cfg)
	result.ForLLM = truncated
}

func (l *Loop) applyRuntimeConstraints(
	state *pipeline.RunState,
	tc providers.ToolCall,
	result *tools.Result,
) []providers.Message {
	if result == nil {
		return nil
	}

	var msgs []providers.Message
	emitConstraint := func(constraint pipeline.Constraint) {
		iteration := 0
		if state != nil {
			iteration = state.Iteration
		}
		normalized := constraint.Normalize(iteration)
		appendToolConstraint(result, normalized)
		if state == nil {
			return
		}
		if state.EnsureConstraintStore().Add(normalized) {
			if normalized.Severity == pipeline.SeverityHard &&
				normalized.Resolution == pipeline.ResolutionHumanRequired {
				state.Turn.ArmNeedsHuman(pipeline.TurnCloseoutReasonConstraintNeedsHuman)
			}
			msgs = append(msgs, providers.Message{
				Role:    "system",
				Content: formatConstraintSystemMessage(normalized),
			})
		}
	}

	for _, constraint := range result.Constraints {
		emitConstraint(constraint)
	}

	if state != nil {
		if recoveryMsg := l.maybeArmReadOnlyExecProbeRecovery(state, tc.Name, tc.Arguments, result); recoveryMsg != nil {
			msgs = append(msgs, *recoveryMsg)
		}
		entry := state.EnsureNoveltyTracker().Record(tc.Name, tc.Arguments, result.ForLLM, result.IsError)
		target := pipeline.PrimaryToolTarget(tc.Name, tc.Arguments)
		switch {
		case target != "" && entry.ConsecutiveSame >= 2:
			emitConstraint(pipeline.Constraint{
				Kind:       pipeline.ConstraintLowSignal,
				Subject:    target,
				Severity:   pipeline.SeveritySoft,
				Resolution: pipeline.ResolutionSelfReroute,
				Sticky:     false,
				Message:    "repeated identical results for this target",
			})
		case target != "" && entry.CallCount >= 3 && entry.ShrinkingCount >= 2:
			emitConstraint(pipeline.Constraint{
				Kind:       pipeline.ConstraintLowSignal,
				Subject:    target,
				Severity:   pipeline.SeveritySoft,
				Resolution: pipeline.ResolutionSelfReroute,
				Sticky:     false,
				Message:    "diminishing returns for this target",
			})
		}
		if entry.ErrorRepeatCount >= 2 {
			emitConstraint(pipeline.Constraint{
				Kind:       pipeline.ConstraintRepeatedFailure,
				Subject:    pipeline.ToolTargetKey(tc.Name, tc.Arguments),
				Severity:   pipeline.SeveritySoft,
				Resolution: pipeline.ResolutionSelfReroute,
				Sticky:     false,
				Message:    "the same error repeated for this tool target",
			})
		}
	}

	return msgs
}

func appendToolConstraint(result *tools.Result, constraint pipeline.Constraint) {
	if result == nil {
		return
	}
	key := constraint.Key()
	for _, existing := range result.Constraints {
		if existing.Key() == key {
			return
		}
	}
	result.Constraints = append(result.Constraints, constraint)
}

func formatConstraintSystemMessage(constraint pipeline.Constraint) string {
	message := fmt.Sprintf("[System] Runtime constraint registered: %s on %s. %s.", constraint.Kind, constraint.Subject, constraint.Message)
	if constraint.Severity == pipeline.SeverityHard {
		message += " Do not retry this blocked path."
	} else {
		message += " Prefer a different approach."
	}
	if constraint.Resolution == pipeline.ResolutionHumanRequired {
		message += " This requires human action to resolve."
	}
	return message
}
