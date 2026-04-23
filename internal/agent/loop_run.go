package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Run processes a single message through the agent loop.
// It blocks until completion and returns the final response.
func (l *Loop) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	l.activeRuns.Add(1)
	defer l.activeRuns.Add(-1)

	// Per-run emit wrapper: enriches every AgentEvent with delegation + routing context.
	emitRun := func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.DelegationID = req.DelegationID
		event.TeamID = req.TeamID
		event.TeamTaskID = req.TeamTaskID
		event.ParentAgentID = req.ParentAgentID
		event.SenderID = req.SenderID
		event.UserID = req.UserID
		event.Channel = req.Channel
		event.ChatID = req.ChatID
		event.SessionKey = req.SessionKey
		event.TenantID = store.TenantIDFromContext(ctx)
		l.emit(event)
	}

	emitRun(AgentEvent{
		Type:    protocol.AgentEventRunStarted,
		AgentID: l.id,
		RunID:   req.RunID,
		Payload: map[string]any{"message": req.Message},
	})

	// Create trace
	var traceID uuid.UUID
	isChildTrace := req.ParentTraceID != uuid.Nil && l.traceCollector != nil

	// agentSpanID holds the pre-generated root agent span ID.
	// Used by emitAgentSpanEnd in the deferred finalizer below.
	var agentSpanID uuid.UUID

	if isChildTrace {
		// Announce run: reuse parent trace, don't create new trace record.
		// Spans will be added to the parent trace with proper nesting.
		traceID = req.ParentTraceID
		ctx = tracing.WithTraceID(ctx, traceID)
		ctx = tracing.WithCollector(ctx, l.traceCollector)
		agentSpanID = store.GenNewID()
		ctx = tracing.WithParentSpanID(ctx, agentSpanID)
		if req.ParentRootSpanID != uuid.Nil {
			ctx = tracing.WithAnnounceParentSpanID(ctx, req.ParentRootSpanID)
		}
	} else if l.traceCollector != nil {
		traceID = store.GenNewID()
		now := time.Now().UTC()
		traceName := "chat " + l.id
		if req.TraceName != "" {
			traceName = req.TraceName
		}
		trace := &store.TraceData{
			ID:           traceID,
			RunID:        req.RunID,
			SessionKey:   req.SessionKey,
			UserID:       req.UserID,
			Channel:      req.Channel,
			Name:         traceName,
			InputPreview: truncateStr(req.Message, l.traceCollector.PreviewMaxLen()),
			Status:       store.TraceStatusRunning,
			StartTime:    now,
			CreatedAt:    now,
			Tags:         req.TraceTags,
		}
		if l.agentUUID != uuid.Nil {
			trace.AgentID = &l.agentUUID
		}
		// Link to parent trace: delegation context or explicit LinkedTraceID (team task runs).
		if delegateParent := tracing.DelegateParentTraceIDFromContext(ctx); delegateParent != uuid.Nil {
			trace.ParentTraceID = &delegateParent
		} else if req.LinkedTraceID != uuid.Nil {
			trace.ParentTraceID = &req.LinkedTraceID
		}
		// Set team_id on trace for team-scoped runs.
		if req.TeamID != "" {
			if tid, err := uuid.Parse(req.TeamID); err == nil {
				trace.TeamID = &tid
			}
		}
		if err := l.traceCollector.CreateTrace(ctx, trace); err != nil {
			slog.Warn("tracing: failed to create trace", "error", err)
		} else {
			ctx = tracing.WithTraceID(ctx, traceID)
			ctx = tracing.WithCollector(ctx, l.traceCollector)
			if trace.TeamID != nil {
				ctx = tracing.WithTraceTeamID(ctx, *trace.TeamID)
			}

			// Notify the gateway so it can associate this traceID with the active run
			// entry for force-abort (forceMarkTraceAborted needs traceID at abort time).
			if req.OnTraceCreated != nil {
				req.OnTraceCreated(traceID)
			}

			// Pre-generate root "agent" span ID so LLM/tool spans can reference it as parent.
			agentSpanID = store.GenNewID()
			ctx = tracing.WithParentSpanID(ctx, agentSpanID)
		}
	}

	// Inject local key into tool context so delegation/subagent tools can
	// propagate topic/thread routing info back through announce messages.
	if req.LocalKey != "" {
		ctx = tools.WithToolLocalKey(ctx, req.LocalKey)
	}

	runStart := time.Now().UTC()

	// Safety net: ensure root traces are ALWAYS finalized, even on panic or goroutine leak.
	// Normal-path finalization sets traceFinalized=true; this defer only acts if it wasn't.
	var traceFinalized bool
	if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
		defer func() {
			if traceFinalized {
				return
			}
			slog.Warn("tracing: safety-net finalizing orphan trace",
				"trace_id", traceID, "agent", l.id, "session", req.SessionKey)
			safeCtx := context.WithoutCancel(ctx)
			if agentSpanID != uuid.Nil {
				l.emitAgentSpanEnd(safeCtx, agentSpanID, runStart, nil, context.Canceled)
			}
			l.traceCollector.FinishTrace(safeCtx, traceID, store.TraceStatusError,
				"trace finalized by safety net (likely panic or goroutine leak)", "")
		}()
	}

	// Emit running agent span immediately so it's visible in the trace UI.
	if agentSpanID != uuid.Nil {
		var agentSpanOpts []spanOption
		if req.ModelOverride != "" {
			agentSpanOpts = append(agentSpanOpts, withModel(req.ModelOverride))
		}
		if req.ProviderOverride != nil {
			agentSpanOpts = append(agentSpanOpts, withProvider(req.ProviderOverride.Name()))
		}
		l.emitAgentSpanStart(ctx, agentSpanID, runStart, req.Message, agentSpanOpts...)
	}

	// Child trace (announce run): set parent trace back to "running" while
	// this run is active so the trace UI doesn't show "completed" with a
	// "running" child span.
	if isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
		l.traceCollector.SetTraceStatus(ctx, traceID, store.TraceStatusRunning)
	}

	// V3 pipeline path (always enabled)
	{
		result, err := l.runViaPipeline(ctx, req)
		// Tracing + events handled below via the same finalize path
		if err != nil {
			if agentSpanID != uuid.Nil {
				l.emitAgentSpanEnd(ctx, agentSpanID, runStart, nil, err)
			}
			if isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
				status := store.TraceStatusError
				if ctx.Err() != nil {
					status = store.TraceStatusCancelled
				}
				traceCtx := ctx
				if ctx.Err() != nil {
					traceCtx = context.WithoutCancel(ctx)
				}
				l.traceCollector.SetTraceStatus(traceCtx, traceID, status)
			}
			if ctx.Err() != nil {
				emitRun(AgentEvent{Type: protocol.AgentEventRunCancelled, AgentID: l.id, RunID: req.RunID})
			} else {
				emitRun(AgentEvent{Type: protocol.AgentEventRunFailed, AgentID: l.id, RunID: req.RunID, Payload: map[string]string{"error": err.Error()}})
			}
			if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
				traceFinalized = true
				traceCtx := ctx
				traceStatus := store.TraceStatusError
				if ctx.Err() != nil {
					traceCtx = context.WithoutCancel(ctx)
					traceStatus = store.TraceStatusCancelled
				}
				l.traceCollector.FinishTrace(traceCtx, traceID, traceStatus, err.Error(), "")
			}
			return nil, err
		}
		// Structured performance log for v3 pipeline runs.
		elapsed := time.Since(runStart)
		logAttrs := []any{
			"agent", l.id, "duration_ms", elapsed.Milliseconds(),
			"iterations", result.Iterations,
		}
		if result.Usage != nil {
			logAttrs = append(logAttrs, "total_tokens", result.Usage.TotalTokens)
		}
		slog.Info("v3.run.completed", logAttrs...)

		if agentSpanID != uuid.Nil {
			l.emitAgentSpanEnd(ctx, agentSpanID, runStart, result, nil)
		}
		if isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
			l.traceCollector.SetTraceStatus(ctx, traceID, store.TraceStatusCompleted)
		}
		completedPayload := map[string]any{"content": result.Content}
		if result.Thinking != "" {
			completedPayload["thinking"] = result.Thinking
		}
		if result != nil && result.Usage != nil {
			completedPayload["usage"] = map[string]any{
				"prompt_tokens":         result.Usage.PromptTokens,
				"completion_tokens":     result.Usage.CompletionTokens,
				"total_tokens":          result.Usage.TotalTokens,
				"cache_creation_tokens": result.Usage.CacheCreationTokens,
				"cache_read_tokens":     result.Usage.CacheReadTokens,
			}
		}
		if result != nil && len(result.Media) > 0 {
			completedPayload["media"] = result.Media
		}
		emitRun(AgentEvent{Type: protocol.AgentEventRunCompleted, AgentID: l.id, RunID: req.RunID, Payload: completedPayload})
		if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
			traceFinalized = true
			if result != nil {
				l.traceCollector.FinishTrace(ctx, traceID, store.TraceStatusCompleted, "", truncateStr(result.Content, l.traceCollector.PreviewMaxLen()))
			} else {
				l.traceCollector.FinishTrace(ctx, traceID, store.TraceStatusCompleted, "", "")
			}
		}
		return result, nil
	}
}
