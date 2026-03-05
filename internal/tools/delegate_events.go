package tools

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// emitDelegationEvent broadcasts a delegation lifecycle event with a typed payload.
func (dm *DelegateManager) emitDelegationEvent(name string, task *DelegationTask) {
	if dm.msgBus == nil {
		return
	}
	dm.msgBus.Broadcast(bus.Event{
		Name:    name,
		Payload: buildDelegationPayload(task),
	})
}

// emitDelegationEventWithError broadcasts a delegation failed event including the error.
func (dm *DelegateManager) emitDelegationEventWithError(task *DelegationTask, err error) {
	if dm.msgBus == nil {
		return
	}
	payload := buildDelegationPayload(task)
	payload.Status = "failed"
	payload.Error = err.Error()
	if task.CompletedAt != nil {
		payload.ElapsedMS = int(task.CompletedAt.Sub(task.CreatedAt).Milliseconds())
	} else {
		payload.ElapsedMS = int(time.Since(task.CreatedAt).Milliseconds())
	}
	dm.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventDelegationFailed,
		Payload: payload,
	})
}

// buildDelegationPayload creates a DelegationEventPayload from a DelegationTask.
func buildDelegationPayload(task *DelegationTask) protocol.DelegationEventPayload {
	payload := protocol.DelegationEventPayload{
		DelegationID:      task.ID,
		SourceAgentID:     task.SourceAgentID.String(),
		SourceAgentKey:    task.SourceAgentKey,
		SourceDisplayName: task.SourceDisplayName,
		TargetAgentID:     task.TargetAgentID.String(),
		TargetAgentKey:    task.TargetAgentKey,
		TargetDisplayName: task.TargetDisplayName,
		UserID:            task.UserID,
		Channel:           task.OriginChannel,
		ChatID:            task.OriginChatID,
		Mode:              task.Mode,
		Task:              task.Task,
		Status:            task.Status,
		CreatedAt:         task.CreatedAt.UTC().Format(time.RFC3339),
	}
	if task.TeamID != uuid.Nil {
		payload.TeamID = task.TeamID.String()
	}
	if task.TeamTaskID != uuid.Nil {
		payload.TeamTaskID = task.TeamTaskID.String()
	}
	if task.CompletedAt != nil {
		payload.ElapsedMS = int(task.CompletedAt.Sub(task.CreatedAt).Milliseconds())
	}
	return payload
}

func formatDelegateAnnounce(task *DelegationTask, artifacts *DelegateArtifacts, err error, elapsed time.Duration) string {
	if err != nil && len(artifacts.Results) == 0 {
		return fmt.Sprintf(
			"[System Message] All delegations finished. The last delegation to agent %q failed.\n\nError: %s\n\nStats: runtime %s\n\n"+
				"IMPORTANT: Before retrying or handling the task yourself, send a brief, friendly message to the user "+
				"letting them know there was a small hiccup and you're working on it. Keep it short and natural (1-2 sentences). "+
				"Then retry the delegation or handle it yourself.",
			task.TargetAgentKey, err.Error(), elapsed.Round(time.Millisecond))
	}

	msg := "[System Message] All team delegations completed.\n\n"

	// List auto-completed task IDs so LLM doesn't reuse them
	if len(artifacts.CompletedTaskIDs) > 0 {
		msg += "Auto-completed team tasks: "
		for i, tid := range artifacts.CompletedTaskIDs {
			if i > 0 {
				msg += ", "
			}
			msg += tid
		}
		msg += "\nFor follow-up delegations, create NEW tasks (do not reuse completed task IDs).\n\n"
	}

	// Render each delegation result
	for i, r := range artifacts.Results {
		msg += fmt.Sprintf("--- Result from %q ---\n%s\n", r.AgentKey, r.Content)
		if len(r.Deliverables) > 0 {
			for _, d := range r.Deliverables {
				preview := d
				if len(preview) > 4000 {
					preview = preview[:4000] + "\n[...truncated, full content in team_tasks]"
				}
				msg += fmt.Sprintf("\n[Deliverable]\n%s\n", preview)
			}
		}
		if r.HasMedia {
			msg += "[media file(s) attached — will be delivered automatically. Do NOT recreate or call create_image.]\n"
		}
		if i < len(artifacts.Results)-1 {
			msg += "\n"
		}
	}

	msg += fmt.Sprintf("\nStats: total elapsed %s\n\n", elapsed.Round(time.Millisecond))
	msg += "Review the results above. You may:\n" +
		"- Present a comprehensive summary to the user (if the task is fully done)\n" +
		"- Delegate follow-up tasks to refine, combine, or extend these results\n" +
		"- Ask a member to revise based on another member's output\n" +
		"Any media files attached will be delivered automatically — do NOT recreate them."

	return msg
}
