package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
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
	ctx = withDelegationArtifactTextRedactor(ctx, &req)

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
		l.emit(redactDelegationAgentEvent(&req, event))
	}

	emitRun(AgentEvent{
		Type:    protocol.AgentEventRunStarted,
		AgentID: l.id,
		RunID:   req.RunID,
		Payload: map[string]any{"message": req.Message},
	})

	// Durable run record: create agent_runs row + start heartbeat (non-fatal).
	// The record is finalized on every exit path below (completed/failed/cancelled).
	runRecord := startRunRecord(ctx, l, req)
	// Safety net: a panic in runViaPipeline would otherwise leak the heartbeat
	// goroutine and leave the run record perpetually "running". terminal() is
	// idempotent, so this cannot conflict with the normal exit path.
	defer runRecord.terminal(ctx, store.AgentRunStatusFailed, "run record finalized by safety net (likely panic or goroutine leak)")

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
			InputPreview: tracing.RedactText(ctx, truncateStr(req.Message, l.traceCollector.PreviewMaxLen())),
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
				tracing.RedactText(safeCtx, "trace finalized by safety net (likely panic or goroutine leak)"), "")
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
		// Durable checkpoint writer: wired into PipelineDeps.WriteCheckpoint so
		// CheckpointStage persists a resumable snapshot at the configured cadence.
		// Non-fatal; nil when the run record is disabled (store not wired).
		// checkpointWritten flips true once a checkpoint has landed in the DB, so
		// the error path can decide between "compacting (resumable)" and "failed".
		var checkpointWriter func(ctx context.Context, state *pipeline.RunState) error
		var checkpointWritten bool
		if runRecord != nil {
			checkpointWriter = func(ctx context.Context, state *pipeline.RunState) error {
				if err := runRecord.checkpoint(ctx, store.AgentRunStatusRunning, state); err != nil {
					return err
				}
				checkpointWritten = true
				return nil
			}
		}
		result, err := l.runViaPipeline(ctx, req, nil, checkpointWriter)
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
				runRecord.terminal(ctx, store.AgentRunStatusCancelled, "cancelled")
			} else {
				// G3: on a transient (non-cancel) failure, keep the run resumable
				// when at least one durable checkpoint landed before the error. The
				// run record transitions to compacting (not failed) so ResumeRun
				// can pick it up; the WS event still reports the failure.
				emitRun(AgentEvent{Type: protocol.AgentEventRunFailed, AgentID: l.id, RunID: req.RunID, Payload: map[string]string{"error": err.Error()}})
				if runRecord != nil && checkpointWritten {
					slog.Warn("run compacted, resumable", "run_id", req.RunID, "error", err)
					runRecord.terminal(ctx, store.AgentRunStatusCompacting, err.Error())
				} else {
					runRecord.terminal(ctx, store.AgentRunStatusFailed, err.Error())
				}
			}
			if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
				traceFinalized = true
				traceCtx := ctx
				traceStatus := store.TraceStatusError
				if ctx.Err() != nil {
					traceCtx = context.WithoutCancel(ctx)
					traceStatus = store.TraceStatusCancelled
				}
				l.traceCollector.FinishTrace(traceCtx, traceID, traceStatus, tracing.RedactText(traceCtx, err.Error()), "")
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
			logAttrs = append(logAttrs,
				"total_tokens", result.Usage.TotalTokens,
				"total_prompt_tokens", result.Usage.PromptTokens,
			)
		}
		if result.LastUsage != nil {
			logAttrs = append(logAttrs, "last_usage_prompt_tokens", result.LastUsage.PromptTokens)
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
				"prompt_tokens":                         result.Usage.PromptTokens,
				"completion_tokens":                     result.Usage.CompletionTokens,
				"total_tokens":                          result.Usage.TotalTokens,
				"cache_creation_tokens":                 result.Usage.CacheCreationTokens,
				"cache_read_tokens":                     result.Usage.CacheReadTokens,
				"prompt_tokens_include_cached_segments": result.Usage.PromptTokensIncludeCachedSegments,
			}
		}
		if result != nil && len(result.Media) > 0 {
			completedPayload["media"] = result.Media
		}
		// Record-only completion verification: attach the verifier outcome to
		// the completed event so operators can observe weak-model signals
		// (empty output, missing deliverable). It never changes the terminal
		// decision below.
		if result != nil && result.Completion() != nil {
			completedPayload["completion"] = map[string]any{
				"complete":   result.Completion().Complete,
				"confidence": result.Completion().Confidence,
				"missing":    result.Completion().Missing,
				"reason":     result.Completion().Reason,
			}
		}
		emitRun(AgentEvent{Type: protocol.AgentEventRunCompleted, AgentID: l.id, RunID: req.RunID, Payload: completedPayload})
		runRecord.terminal(ctx, store.AgentRunStatusCompleted, "")
		if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
			traceFinalized = true
			if result != nil {
				// Append the record-only completion verdict to the trace preview so
				// weak-model signals surface in the trace UI without a schema change.
				preview := truncateStr(result.Content, l.traceCollector.PreviewMaxLen())
				if c := result.Completion(); c != nil && !c.Complete {
					preview += "\n[completion: " + c.Reason + "]"
				}
				l.traceCollector.FinishTrace(ctx, traceID, store.TraceStatusCompleted, "",
					tracing.RedactText(ctx, preview))
			} else {
				l.traceCollector.FinishTrace(ctx, traceID, store.TraceStatusCompleted, "", "")
			}
		}
		return result, nil
	}
}

// ErrRunResumeUnavailable is returned by ResumeRun when durable run records are
// not wired (runsStore nil). Distinct from any transient store error so callers
// can surface "resume not supported" cleanly.
var ErrRunResumeUnavailable = errors.New("run resume unavailable: durable run records not wired")

// ErrRunResumeNotFound is returned by ResumeRun when the run record does not
// exist or belongs to no resumable checkpoint.
var ErrRunResumeNotFound = errors.New("run resume failed: run not found or not resumable")

// ResumeRun restores a checkpointed run and drives it through the pipeline
// again without re-running setup stages. It reads the run record, restores the
// pipeline state from the stored checkpoint, rebuilds the RunRequest from the
// record + checkpoint input, and runs the pipeline from the checkpoint's
// iteration. A corrupt/unparseable checkpoint falls back to starting the run
// from scratch (fresh RunState) so resume never hard-fails on old data.
func (l *Loop) ResumeRun(ctx context.Context, runID string) (*RunResult, error) {
	if l.runsStore == nil {
		return nil, ErrRunResumeUnavailable
	}
	if runID == "" {
		return nil, errors.New("resume run: run_id required")
	}
	run, err := l.runsStore.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrRunResumeNotFound
	}

	// Restore the pipeline state. Failing here (corrupt/empty checkpoint) is not
	// fatal: the run starts fresh, losing only its in-flight progress.
	var state *pipeline.RunState
	var savedInput *pipeline.RunInput
	if len(run.Checkpoint) > 0 {
		// RestoreCheckpoint intentionally does NOT restore Input (the caller
		// resolves it); the checkpoint JSON still carries it, so extract it to
		// rebuild the RunRequest's message/channel identity for callbacks.
		savedInput = checkpointRunInput(run.Checkpoint)
		state, err = pipeline.RestoreCheckpoint(run.Checkpoint)
		if err != nil {
			slog.Warn("runs.resume_restore_failed", "run_id", runID, "error", err)
			state = nil // fall through to fresh start
		}
	}

	req := runRequestFromRunRecord(run, savedInput)
	// Resume keeps the existing run row alive with a heartbeat WITHOUT recreating
	// it: CreateRun's ON CONFLICT upsert would clobber the stored checkpoint with
	// NULL. newRunRecordUpdater only starts the heartbeat goroutine. nil when the
	// store is not wired (then checkpoints are also disabled).
	resumeRecord := newRunRecordUpdater(ctx, l, runID)
	defer resumeRecord.terminal(ctx, store.AgentRunStatusFailed, "resume finalized by safety net (likely panic or goroutine leak)")

	// Durable checkpoint writer for the resumed execution: continue updating the
	// same run checkpoint so a re-failure stays resumable. checkpointWritten
	// tracks whether a checkpoint landed during this resume so the error path can
	// decide between compacting (still resumable) and terminal-failed.
	var checkpointWriter func(ctx context.Context, s *pipeline.RunState) error
	var checkpointWritten bool
	if l.runsStore != nil {
		runsStoreSnapshot := l.runsStore
		checkpointWriter = func(ctx context.Context, s *pipeline.RunState) error {
			raw, err := s.MarshalCheckpoint()
			if err != nil {
				slog.Warn("runs.resume_checkpoint_marshal_failed", "run_id", runID, "error", err)
				return err
			}
			if err := runsStoreSnapshot.UpdateRunCheckpoint(ctx, runID, store.AgentRunStatusRunning, raw); err != nil {
				return err
			}
			checkpointWritten = true
			return nil
		}
	}
	result, err := l.runViaPipeline(ctx, req, state, checkpointWriter)
	if err != nil {
		// Finalize the resumed run: a re-failure that still holds a checkpoint
		// stays resumable (compacting), otherwise it is terminal-failed.
		if checkpointWritten {
			slog.Warn("resumed run compacted, resumable", "run_id", runID, "error", err)
			resumeRecord.terminal(ctx, store.AgentRunStatusCompacting, err.Error())
		} else {
			resumeRecord.terminal(ctx, store.AgentRunStatusFailed, err.Error())
		}
		return nil, err
	}
	resumeRecord.terminal(ctx, store.AgentRunStatusCompleted, "")
	return result, nil
}

// runRequestFromRunRecord rebuilds a RunRequest from a stored run record plus,
// when available, the checkpoint's saved input (message/channel/media identity).
// Identity fields the record persists (session, user, channel, chat) are taken
// from the record; fields the checkpoint tracks (message, run kind, workspace
// scope) come from the saved input.
func runRequestFromRunRecord(run *store.AgentRun, savedInput *pipeline.RunInput) RunRequest {
	req := RunRequest{
		RunID:      run.RunID,
		SessionKey: run.SessionKey,
		UserID:     run.UserID,
		Channel:    run.Channel,
		ChatID:     run.ChatID,
	}
	// state.Input may be nil after RestoreCheckpoint (Input is not restored);
	// savedInput carries it directly from the checkpoint JSON.
	if savedInput != nil {
		in := savedInput
		req.Message = in.Message
		req.Media = in.Media
		req.ForwardMedia = in.ForwardMedia
		req.ChannelType = in.ChannelType
		req.BitrixPortalDomain = in.BitrixPortalDomain
		req.ChatTitle = in.ChatTitle
		req.PeerKind = in.PeerKind
		req.SenderID = in.SenderID
		req.SenderName = in.SenderName
		req.Stream = in.Stream
		req.ExtraSystemPrompt = in.ExtraSystemPrompt
		req.SkillFilter = in.SkillFilter
		req.HistoryLimit = in.HistoryLimit
		req.ToolAllow = in.ToolAllow
		req.TelegramManagerPermissions = in.TelegramManagerPermissions
		req.LightContext = in.LightContext
		req.RunKind = in.RunKind
		req.DelegationID = in.DelegationID
		req.TeamID = in.TeamID
		req.TeamTaskID = in.TeamTaskID
		req.ParentAgentID = in.ParentAgentID
		req.MaxIterations = in.MaxIterations
		req.ModelOverride = in.ModelOverride
		req.HideInput = in.HideInput
		req.ContentSuffix = in.ContentSuffix
		req.LeaderAgentID = in.LeaderAgentID
		req.WorkspaceChannel = in.WorkspaceChannel
		req.WorkspaceChatID = in.WorkspaceChatID
		req.TeamWorkspace = in.TeamWorkspace
	}
	return req
}

// checkpointRunInput extracts the saved pipeline input from a checkpoint JSON
// blob. RestoreCheckpoint deliberately drops Input; the serialized checkpoint
// still carries it under "input". Returns nil when absent/unparseable so
// callers fall back to identity-only requests.
func checkpointRunInput(raw json.RawMessage) *pipeline.RunInput {
	var wrapper struct {
		Input *pipeline.RunInput `json:"input"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil
	}
	return wrapper.Input
}
