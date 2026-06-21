package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
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
		tc = l.normalizeToolCall(tc)
		registryName := l.canonicalToolName(l.resolveToolCallName(tc.Name))
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
		toolSpanID := l.emitToolSpanStart(ctx, toolStart, registryName, tc.ID, string(argsJSON))

		// Inject agent audio snapshot so TTS tool (and any future audio consumers)
		// can read agent-level voice/model config without an extra DB lookup.
		if l.agentUUID != uuid.Nil {
			ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
				AgentID:     l.agentUUID,
				OtherConfig: append([]byte(nil), l.agentOtherConfig...), // defensive copy at dispatch
			})
		}
		ctx = store.WithChannelContextScope(ctx, channelContextScopeForRun(req))

		// C2 fix: route through executeToolForActor so per-user MCP tools
		// resolve to the calling user's BridgeTool (not the first user's
		// BridgeTool leaked via shared registry).
		actorUserID := resolveActorUserID(req.UserID, req.SenderID, req.PeerKind, req.ChannelType)
		result := l.executeToolForActor(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, actorUserID)
		toolDuration := time.Since(toolStart)

		l.emitToolSpanEnd(ctx, toolSpanID, toolStart, result)
		l.recordToolUsageEvent(ctx, req, registryName, tc.Name, tc.ID, tc.Arguments, toolStart, result, toolSpanID)

		// v3 evolution metrics: record tool execution non-blocking (best-effort).
		l.recordToolMetric(ctx, req.SessionKey, registryName, !result.IsError, toolDuration)
		if registryName == "use_skill" {
			l.recordSkillUsageFromTool(ctx, req, tc, result, toolDuration)
		}

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
	start    time.Time
	spanID   uuid.UUID
	toolName string
	rawName  string
}

// makeExecuteToolRaw wraps tool I/O only (parallel-safe, no state mutation).
// Returns tool message + toolRawResult (with timing + spanID) as opaque raw data for ProcessToolResult.
func (l *Loop) makeExecuteToolRaw(req *RunRequest) func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
	emitRun := makeToolEmitRun(l, req)
	return func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
		tc = l.normalizeToolCall(tc)
		registryName := l.canonicalToolName(l.resolveToolCallName(tc.Name))
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
		spanID := l.emitToolSpanStart(ctx, start, registryName, tc.ID, string(argsJSON))

		// Inject agent audio snapshot (parallel path — same as sequential makeExecuteToolCall).
		if l.agentUUID != uuid.Nil {
			ctx = store.WithAgentAudio(ctx, store.AgentAudioSnapshot{
				AgentID:     l.agentUUID,
				OtherConfig: append([]byte(nil), l.agentOtherConfig...), // defensive copy at dispatch
			})
		}
		ctx = store.WithChannelContextScope(ctx, channelContextScopeForRun(req))

		// C2 fix (parallel path): route through executeToolForActor for per-user
		// MCP tool isolation. Same rationale as makeExecuteToolCall above.
		actorUserID := resolveActorUserID(req.UserID, req.SenderID, req.PeerKind, req.ChannelType)
		result := l.executeToolForActor(ctx, registryName, tc.Arguments,
			req.Channel, req.ChatID, req.PeerKind, req.SessionKey, actorUserID)
		dur := time.Since(start)

		// Emit tool span end inside goroutine to prevent orphaned spans on ctx cancellation.
		l.emitToolSpanEnd(ctx, spanID, start, result)

		msg := providers.Message{
			Role:       "tool",
			Content:    result.ForLLM,
			ToolCallID: tc.ID,
			IsError:    result.IsError,
		}
		return msg, &toolRawResult{result: result, duration: dur, start: start, spanID: spanID, toolName: registryName, rawName: tc.Name}, nil
	}
}

func channelContextScopeForRun(req *RunRequest) store.ChannelContextScope {
	if req == nil || req.Channel == "" {
		return store.ChannelContextScope{}
	}
	scope := store.ChannelContextScope{
		ChannelInstanceName: req.Channel,
		ScopeType:           store.ChannelScopeTypeChannel,
		ScopeKey:            req.Channel,
	}
	if req.PeerKind == "group" && req.ChatID != "" {
		scope.ScopeType = store.ChannelScopeTypeGroup
		scope.ScopeKey = req.ChatID
	}
	return scope
}

// makeProcessToolResult wraps post-execution bookkeeping (sequential, mutates bridgeRS).
// rawData is *toolRawResult from ExecuteToolRaw — no re-execution.
func (l *Loop) makeProcessToolResult(req *RunRequest, bridgeRS *runState) func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
	emitRun := makeToolEmitRun(l, req)
	return func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message {
		tc = l.normalizeToolCall(tc)
		registryName := l.canonicalToolName(l.resolveToolCallName(tc.Name))

		// Extract result and timing from toolRawResult wrapper.
		var result *tools.Result
		var dur time.Duration
		var start time.Time
		var spanID uuid.UUID
		var rawName string
		if raw, ok := rawData.(*toolRawResult); ok && raw != nil {
			result = raw.result
			dur = raw.duration
			start = raw.start
			spanID = raw.spanID
			registryName = raw.toolName
			rawName = raw.rawName
		} else if r, ok := rawData.(*tools.Result); ok {
			result = r // backward compat
		}
		if result == nil {
			return []providers.Message{rawMsg}
		}
		if rawName == "" {
			rawName = tc.Name
		}
		if !start.IsZero() {
			l.recordToolUsageEvent(ctx, req, registryName, rawName, tc.ID, tc.Arguments, start, result, spanID)
		}

		// Record tool metrics (non-blocking, best-effort).
		l.recordToolMetric(ctx, req.SessionKey, registryName, !result.IsError, dur)
		if registryName == "use_skill" {
			l.recordSkillUsageFromTool(ctx, req, tc, result, dur)
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
				Prompt:      mr.Prompt,
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

func (l *Loop) recordSkillUsageFromTool(ctx context.Context, req *RunRequest, tc providers.ToolCall, result *tools.Result, duration time.Duration) {
	name, _ := tc.Arguments["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	status := store.SkillUsageStatusSucceeded
	reason := ""
	if result != nil && result.IsError {
		status = store.SkillUsageStatusFailed
		reason = strings.TrimSpace(result.ForLLM)
		if len(reason) > 500 {
			reason = reason[:500]
		}
	}
	l.recordSkillUsage(ctx, req, name, tc.ID, "use_skill", status, reason, duration)
}

func (l *Loop) recordSkillUsage(ctx context.Context, req *RunRequest, skillName, invocationID, source, status, reason string, duration time.Duration) {
	if l.skillEvolutionStore == nil || l.skillStore == nil {
		return
	}
	info, ok := l.skillStore.GetSkill(ctx, skillName)
	if !ok || info == nil || info.ID == "" {
		return
	}
	skillID, err := uuid.Parse(info.ID)
	if err != nil {
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = l.tenantID
	}
	metric := store.SkillUsageMetric{
		ID:               uuid.New(),
		SkillID:          skillID,
		SkillSlug:        info.Slug,
		SkillVersion:     info.Version,
		AgentID:          l.agentUUID,
		InvocationID:     invocationID,
		InvocationSource: source,
		Status:           status,
		FailureReason:    reason,
		DurationMs:       duration.Milliseconds(),
	}
	if req != nil {
		metric.UserID = req.UserID
		metric.SessionKey = req.SessionKey
		if req.RunID != "" {
			metric.TraceID = req.RunID
		}
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		if err := l.skillEvolutionStore.RecordUsage(bgCtx, metric); err != nil {
			slog.Debug("skill.metric.record_failed", "skill", skillName, "source", source, "error", err)
		}
	}()
}

// makeToolEmitRun creates a tool event emitter with request context.
func makeToolEmitRun(l *Loop, req *RunRequest) func(AgentEvent) {
	return func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.DelegationID = req.DelegationID
		event.TeamID = req.TeamID
		event.TeamTaskID = req.TeamTaskID
		event.ParentAgentID = req.ParentAgentID
		event.SessionKey = req.SessionKey
		event.SenderID = req.SenderID
		event.UserID = req.UserID
		event.Channel = req.Channel
		event.ChatID = req.ChatID
		event.TenantID = l.tenantID
		l.emit(event)
	}
}
