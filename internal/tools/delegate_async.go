package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// DelegateAsync spawns a delegation in the background and announces the result back.
func (dm *DelegateManager) DelegateAsync(ctx context.Context, opts DelegateOpts) (*DelegateResult, error) {
	task, _, err := dm.prepareDelegation(ctx, opts, "async")
	if err != nil {
		return nil, err
	}

	taskCtx, taskCancel := context.WithCancel(context.Background())
	task.cancelFunc = taskCancel
	dm.active.Store(task.ID, task)

	// Capture parent trace ID before goroutine (ctx.Background() loses it)
	parentTraceID := tracing.TraceIDFromContext(ctx)
	if parentTraceID != uuid.Nil {
		taskCtx = tracing.WithDelegateParentTraceID(taskCtx, parentTraceID)
	}

	dm.injectDependencyResults(ctx, &opts)
	dm.injectWorkspaceContext(ctx, task, &opts)
	message := buildDelegateMessage(opts)
	dm.emitDelegationEvent(protocol.EventDelegationStarted, task)
	slog.Info("delegation started (async)", "id", task.ID, "target", opts.TargetAgentKey)

	runReq := dm.buildRunRequest(task, message)

	go func() {
		defer func() {
			now := time.Now()
			task.CompletedAt = &now
			dm.active.Delete(task.ID)
		}()

		// Periodic progress notifications — tick every interval until runAgent returns
		// or the delegation is cancelled. Listens on both progressDone (normal exit)
		// and taskCtx.Done() (cancel/stopall) to avoid goroutine leaks.
		progressDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(defaultProgressInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					dm.sendProgressNotification(task)
				case <-progressDone:
					return
				case <-taskCtx.Done():
					return
				}
			}
		}()

		startTime := time.Now()
		result, runErr := dm.runAgent(taskCtx, opts.TargetAgentKey, runReq)
		close(progressDone)
		duration := time.Since(startTime)

		// Count sibling delegations still running (exclude self).
		// Scoped by origin (channel + chatID) so delegations from different
		// conversations are NOT treated as siblings of each other.
		oKey := task.originKey()
		siblings := dm.ListActiveForOrigin(oKey)
		siblingCount := 0
		for _, s := range siblings {
			if s.ID != task.ID {
				siblingCount++
			}
		}
		// Announce result to parent via message bus
		if dm.msgBus != nil && task.OriginChannel != "" {
			elapsed := time.Since(task.CreatedAt)

			if siblingCount > 0 {
				// Intermediate completion: accumulate artifacts + result summary.
				// The final announce includes all sibling results so the lead doesn't
				// need to call team_tasks to aggregate.
				arts := &DelegateArtifacts{}
				if result != nil {
					arts.Media = result.Media
					arts.Results = []DelegateResultSummary{{
						AgentKey:     task.TargetAgentKey,
						DisplayName:  task.TargetDisplayName,
						Content:      result.Content,
						HasMedia:     len(result.Media) > 0,
						Deliverables: result.Deliverables,
					}}
				} else if runErr != nil {
					arts.Results = []DelegateResultSummary{{
						AgentKey:    task.TargetAgentKey,
						DisplayName: task.TargetDisplayName,
						Content:     fmt.Sprintf("[failed] %s", runErr.Error()),
					}}
				}
				if task.TeamTaskID != uuid.Nil {
					arts.CompletedTaskIDs = []string{task.TeamTaskID.String()}
				}
				dm.accumulateArtifacts(oKey, arts)

				// Emit accumulated event so WS clients know this delegation finished
				// but results are being held until siblings complete.
				if dm.msgBus != nil {
					dm.msgBus.Broadcast(bus.Event{
						Name: protocol.EventDelegationAccumulated,
						Payload: protocol.DelegationAccumulatedPayload{
							DelegationID:      task.ID,
							SourceAgentID:     task.SourceAgentID.String(),
							SourceAgentKey:    task.SourceAgentKey,
							TargetAgentKey:    task.TargetAgentKey,
							TargetDisplayName: task.TargetDisplayName,
							UserID:            task.UserID,
							Channel:           task.OriginChannel,
							ChatID:            task.OriginChatID,
							TeamID:            func() string { if task.TeamID != uuid.Nil { return task.TeamID.String() }; return "" }(),
							TeamTaskID:        func() string { if task.TeamTaskID != uuid.Nil { return task.TeamTaskID.String() }; return "" }(),
							SiblingsRemaining: siblingCount,
							ElapsedMS:         int(time.Since(task.CreatedAt).Milliseconds()),
						},
					})
				}
				slog.Info("delegation announce suppressed (siblings still running)",
					"id", task.ID, "target", task.TargetAgentKey, "siblings", siblingCount)
			} else {
				// Last completion: clear progress dedup so next batch gets fresh notifications.
				dm.progressSent.Delete(task.SourceAgentID.String() + ":" + task.OriginChatID)

				// Last completion: collect all accumulated artifacts + own result
				artifacts := dm.collectArtifacts(oKey)
				if result != nil {
					artifacts.Media = append(artifacts.Media, result.Media...)
					artifacts.Results = append(artifacts.Results, DelegateResultSummary{
						AgentKey:     task.TargetAgentKey,
						DisplayName:  task.TargetDisplayName,
						Content:      result.Content,
						HasMedia:     len(result.Media) > 0,
						Deliverables: result.Deliverables,
					})
				}
				if task.TeamTaskID != uuid.Nil {
					artifacts.CompletedTaskIDs = append(artifacts.CompletedTaskIDs, task.TeamTaskID.String())
				}

				announceMeta := map[string]string{
					"origin_channel":      task.OriginChannel,
					"origin_peer_kind":    task.OriginPeerKind,
					"parent_agent":        task.SourceAgentKey,
					"delegation_id":       task.ID,
					"target_agent":        task.TargetAgentKey,
					"origin_trace_id":     task.OriginTraceID.String(),
					"origin_root_span_id": task.OriginRootSpanID.String(),
				}
				if task.OriginLocalKey != "" {
					announceMeta["origin_local_key"] = task.OriginLocalKey
				}
				if task.OriginSessionKey != "" {
					announceMeta["origin_session_key"] = task.OriginSessionKey
				}
				// Emit announce event so WS clients know all results are being sent to lead.
				hasMedia := len(artifacts.Media) > 0
				var announceSummaries []protocol.DelegationAnnounceResultSummary
				for _, r := range artifacts.Results {
					preview := r.Content
					if runes := []rune(preview); len(runes) > 200 {
						preview = string(runes[:200]) + "..."
					}
					announceSummaries = append(announceSummaries, protocol.DelegationAnnounceResultSummary{
						AgentKey:       r.AgentKey,
						DisplayName:    r.DisplayName,
						HasMedia:       r.HasMedia,
						ContentPreview: preview,
					})
					if r.HasMedia {
						hasMedia = true
					}
				}
				dm.msgBus.Broadcast(bus.Event{
					Name: protocol.EventDelegationAnnounce,
					Payload: protocol.DelegationAnnouncePayload{
						SourceAgentID:     task.SourceAgentID.String(),
						SourceAgentKey:    task.SourceAgentKey,
						SourceDisplayName: task.SourceDisplayName,
						UserID:            task.UserID,
						Channel:        task.OriginChannel,
						ChatID:         task.OriginChatID,
						TeamID:         func() string { if task.TeamID != uuid.Nil { return task.TeamID.String() }; return "" }(),
						Results:        announceSummaries,
						CompletedTaskIDs: artifacts.CompletedTaskIDs,
						TotalElapsedMS: int(elapsed.Milliseconds()),
						HasMedia:       hasMedia,
					},
				})

				announceMsg := bus.InboundMessage{
					Channel:  "system",
					SenderID: fmt.Sprintf("delegate:%s", task.ID),
					ChatID:   task.OriginChatID,
					Content:  formatDelegateAnnounce(task, artifacts, runErr, elapsed),
					UserID:   task.UserID,
					Metadata: announceMeta,
					Media:    artifacts.Media,
				}
				dm.msgBus.PublishInbound(announceMsg)
			}
		}

		if runErr != nil {
			task.Status = "failed"
			dm.autoFailTeamTask(task, runErr.Error())
			dm.emitDelegationEventWithError(task, runErr)
			dm.saveDelegationHistory(task, "", runErr, duration)
		} else {
			// Apply quality gates before marking completed.
			if result, runErr = dm.applyQualityGates(taskCtx, task, opts, result); runErr != nil {
				task.Status = "failed"
				dm.autoFailTeamTask(task, runErr.Error())
				dm.emitDelegationEventWithError(task, runErr)
				dm.saveDelegationHistory(task, "", runErr, duration)
			} else {
				task.Status = "completed"
				dm.emitDelegationEvent(protocol.EventDelegationCompleted, task)
				dm.trackCompleted(task)
				resultContent := ""
				var deliverables []string
				if result != nil {
					resultContent = result.Content
					deliverables = result.Deliverables
				}
				// Auto-complete the team task for EVERY delegation (not just the last one).
				// Each delegation has its own TeamTaskID — the isLastDelegation guard
				// is for announce batching only, not for task completion.
				dm.autoCompleteTeamTask(task, resultContent, deliverables)
				dm.saveDelegationHistory(task, resultContent, nil, duration)
			}
		}
		slog.Info("delegation finished (async)", "id", task.ID, "target", task.TargetAgentKey, "status", task.Status)
	}()

	return &DelegateResult{DelegationID: task.ID, TeamTaskID: task.TeamTaskID.String()}, nil
}
