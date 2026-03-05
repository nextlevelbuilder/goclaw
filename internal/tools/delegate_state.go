package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// CancelForOrigin cancels all active async delegations originating from a given channel + chatID.
// Used by /stopall to stop delegate tasks that bypass the scheduler.
func (dm *DelegateManager) CancelForOrigin(channel, chatID string) int {
	count := 0
	dm.active.Range(func(key, val any) bool {
		t := val.(*DelegationTask)
		if t.Status == "running" && t.OriginChannel == channel && t.OriginChatID == chatID {
			if t.cancelFunc != nil {
				t.cancelFunc()
			}
			t.Status = "cancelled"
			now := time.Now()
			t.CompletedAt = &now
			dm.active.Delete(key)
			dm.emitDelegationEvent(protocol.EventDelegationCancelled, t)
			slog.Info("delegation cancelled by /stopall", "id", t.ID, "target", t.TargetAgentKey)
			count++
		}
		return true
	})
	return count
}

// Cancel cancels a running delegation by ID.
func (dm *DelegateManager) Cancel(delegationID string) bool {
	val, ok := dm.active.Load(delegationID)
	if !ok {
		return false
	}
	task := val.(*DelegationTask)
	if task.cancelFunc != nil {
		task.cancelFunc()
	}
	task.Status = "cancelled"
	now := time.Now()
	task.CompletedAt = &now
	dm.active.Delete(delegationID)
	dm.emitDelegationEvent(protocol.EventDelegationCancelled, task)
	slog.Info("delegation cancelled", "id", delegationID, "target", task.TargetAgentKey)
	return true
}

// CancelByTeamTaskID cancels a running delegation associated with a team task.
// Returns true if a delegation was found and cancelled.
func (dm *DelegateManager) CancelByTeamTaskID(teamTaskID uuid.UUID) bool {
	found := false
	dm.active.Range(func(key, val any) bool {
		t := val.(*DelegationTask)
		if t.TeamTaskID == teamTaskID && t.Status == "running" {
			if t.cancelFunc != nil {
				t.cancelFunc()
			}
			t.Status = "cancelled"
			now := time.Now()
			t.CompletedAt = &now
			dm.active.Delete(key)
			dm.emitDelegationEvent(protocol.EventDelegationCancelled, t)
			slog.Info("delegation cancelled by team task cancel",
				"delegation_id", t.ID, "team_task_id", teamTaskID, "target", t.TargetAgentKey)
			found = true
			return false // stop iteration
		}
		return true
	})
	return found
}

// ListActive returns all active delegations for a source agent.
func (dm *DelegateManager) ListActive(sourceAgentID uuid.UUID) []*DelegationTask {
	var tasks []*DelegationTask
	dm.active.Range(func(_, val any) bool {
		t := val.(*DelegationTask)
		if t.SourceAgentID == sourceAgentID && t.Status == "running" {
			tasks = append(tasks, t)
		}
		return true
	})
	return tasks
}

// ActiveCountForLink counts running delegations for a specific source→target pair.
func (dm *DelegateManager) ActiveCountForLink(sourceID, targetID uuid.UUID) int {
	count := 0
	dm.active.Range(func(_, val any) bool {
		t := val.(*DelegationTask)
		if t.SourceAgentID == sourceID && t.TargetAgentID == targetID && t.Status == "running" {
			count++
		}
		return true
	})
	return count
}

// ActiveCountForTarget counts running delegations targeting a specific agent from all sources.
func (dm *DelegateManager) ActiveCountForTarget(targetID uuid.UUID) int {
	count := 0
	dm.active.Range(func(_, val any) bool {
		t := val.(*DelegationTask)
		if t.TargetAgentID == targetID && t.Status == "running" {
			count++
		}
		return true
	})
	return count
}

// accumulateArtifacts merges new artifacts into the pending set for a source agent.
// Called for intermediate delegation completions (when siblings are still running).
func (dm *DelegateManager) accumulateArtifacts(sourceAgentID uuid.UUID, arts *DelegateArtifacts) {
	key := sourceAgentID.String()
	existing, _ := dm.pendingArtifacts.Load(key)
	var merged DelegateArtifacts
	if existing != nil {
		merged = *existing.(*DelegateArtifacts)
	}
	merged.Media = append(merged.Media, arts.Media...)
	merged.Results = append(merged.Results, arts.Results...)
	merged.CompletedTaskIDs = append(merged.CompletedTaskIDs, arts.CompletedTaskIDs...)
	dm.pendingArtifacts.Store(key, &merged)
}

// collectArtifacts retrieves and removes all accumulated artifacts for a source agent.
// Called when the last delegation completes (siblingCount == 0).
func (dm *DelegateManager) collectArtifacts(sourceAgentID uuid.UUID) *DelegateArtifacts {
	key := sourceAgentID.String()
	if pending, ok := dm.pendingArtifacts.LoadAndDelete(key); ok {
		return pending.(*DelegateArtifacts)
	}
	return &DelegateArtifacts{}
}

// trackCompleted records a delegate session key for deferred cleanup.
func (dm *DelegateManager) trackCompleted(task *DelegationTask) {
	if dm.sessionStore == nil {
		return
	}
	dm.completedMu.Lock()
	dm.completedSessions = append(dm.completedSessions, task.SessionKey)
	dm.completedMu.Unlock()
}

// flushCompletedSessions deletes all tracked delegate sessions.
func (dm *DelegateManager) flushCompletedSessions() {
	if dm.sessionStore == nil {
		return
	}
	dm.completedMu.Lock()
	sessions := dm.completedSessions
	dm.completedSessions = nil
	dm.completedMu.Unlock()

	for _, key := range sessions {
		if err := dm.sessionStore.Delete(key); err != nil {
			slog.Warn("delegate: session cleanup failed", "session", key, "error", err)
		}
	}
	if len(sessions) > 0 {
		slog.Info("delegate: cleaned up sessions", "count", len(sessions))
	}
}

// autoCompleteTeamTask attempts to claim+complete the associated team task.
// Called after a delegation finishes successfully. Errors are logged but not fatal.
// On success, flushes all tracked delegate sessions (task done = context no longer needed).
// Also persists a team message record for audit trail / visualization.
func (dm *DelegateManager) autoCompleteTeamTask(task *DelegationTask, resultContent string, deliverables []string) {
	if dm.teamStore == nil || task.TeamTaskID == uuid.Nil {
		return
	}

	// Use actual deliverables (tool outputs) as task result when available,
	// falling back to the LLM's summary response.
	taskResult := resultContent
	if len(deliverables) > 0 {
		taskResult = strings.Join(deliverables, "\n\n---\n\n")
		if len(taskResult) > 100_000 {
			taskResult = taskResult[:100_000] + "\n\n[truncated]"
		}
	}

	_ = dm.teamStore.ClaimTask(context.Background(), task.TeamTaskID, task.TargetAgentID, task.TeamID)
	if err := dm.teamStore.CompleteTask(context.Background(), task.TeamTaskID, task.TeamID, taskResult); err != nil {
		slog.Warn("delegate: failed to auto-complete team task",
			"task_id", task.TeamTaskID, "delegation_id", task.ID, "error", err)
	} else {
		slog.Info("delegate: auto-completed team task",
			"task_id", task.TeamTaskID, "delegation_id", task.ID)
		// Task done — flush delegate sessions
		dm.flushCompletedSessions()

		// Persist delegation completion as team message for audit trail
		if task.TeamID != uuid.Nil {
			summary := resultContent
			if len(summary) > 500 {
				summary = summary[:500] + "..."
			}
			taskID := task.TeamTaskID
			_ = dm.teamStore.SendMessage(context.Background(), &store.TeamMessageData{
				TeamID:      task.TeamID,
				FromAgentID: task.TargetAgentID,
				ToAgentID:   &task.SourceAgentID,
				Content:     fmt.Sprintf("[Delegation completed] %s", summary),
				MessageType: store.TeamMessageTypeChat,
				TaskID:      &taskID,
			})
		}
	}
}

// saveDelegationHistory persists a delegation record to the database.
// Called after delegation completes (success, fail, or cancel). Errors are logged, not fatal.
func (dm *DelegateManager) saveDelegationHistory(task *DelegationTask, resultContent string, delegateErr error, duration time.Duration) {
	if dm.teamStore == nil {
		return
	}

	record := &store.DelegationHistoryData{
		SourceAgentID: task.SourceAgentID,
		TargetAgentID: task.TargetAgentID,
		UserID:        task.UserID,
		Task:          task.Task,
		Mode:          task.Mode,
		Iterations:    0,
		DurationMS:    int(duration.Milliseconds()),
	}

	if task.TeamID != uuid.Nil {
		record.TeamID = &task.TeamID
	}
	if task.TeamTaskID != uuid.Nil {
		record.TeamTaskID = &task.TeamTaskID
	}
	if task.OriginTraceID != uuid.Nil {
		record.TraceID = &task.OriginTraceID
	}

	now := time.Now()
	record.CompletedAt = &now

	if delegateErr != nil {
		record.Status = "failed"
		errStr := delegateErr.Error()
		record.Error = &errStr
	} else {
		record.Status = "completed"
		record.Result = &resultContent
	}

	if err := dm.teamStore.SaveDelegationHistory(context.Background(), record); err != nil {
		slog.Warn("delegate: failed to save delegation history",
			"delegation_id", task.ID, "error", err)
	}
}
