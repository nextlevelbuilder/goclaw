package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// memberTaskInfo holds cached task metadata for mid-loop progress nudges.
type memberTaskInfo struct {
	Subject    string
	TaskNumber int
}

// injectTeamTaskReminders adds leader pending-task reminders and
// member progress context to the message list before the first LLM call.
// Returns updated messages and member task info (for progress nudges).
func (l *Loop) injectTeamTaskReminders(ctx context.Context, req *RunRequest, messages []providers.Message) ([]providers.Message, memberTaskInfo) {
	// 2g. Cross-session task reminder: notify team leads about pending and in-progress tasks.
	// Stale recovery (expired lock → pending) is handled by the background TaskTicker.
	// Reminders are injected BEFORE the user message so the user's actual message is always
	// the last message — prevents trailing assistant messages that proxy providers reject.
	if l.teamStore != nil && l.agentUUID != uuid.Nil {
		if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil && team.LeadAgentID == l.agentUUID {
			if tasks, err := l.teamStore.ListTasks(ctx, team.ID, "newest", "active", req.UserID, "", "", 0, 0); err == nil {
				var stale []string
				var inProgress []string
				for _, t := range tasks {
					if t.Status == store.TeamTaskStatusPending {
						age := time.Since(t.CreatedAt).Truncate(time.Minute)
						stale = append(stale, fmt.Sprintf("- %s: \"%s\" (pending %s)", t.ID, t.Subject, age))
					}
					if t.Status == store.TeamTaskStatusInProgress {
						age := time.Since(t.UpdatedAt).Truncate(time.Minute)
						progressInfo := fmt.Sprintf("in progress %s", age)
						if t.ProgressPercent > 0 {
							if t.ProgressStep != "" {
								progressInfo = fmt.Sprintf("%d%% — %s, %s", t.ProgressPercent, t.ProgressStep, age)
							} else {
								progressInfo = fmt.Sprintf("%d%%, %s", t.ProgressPercent, age)
							}
						}
						inProgress = append(inProgress, fmt.Sprintf("- %s: \"%s\" (%s)", t.ID, t.Subject, progressInfo))
					}
				}
				var parts []string
				if len(stale) > 0 {
					parts = append(parts, fmt.Sprintf(
						"You have %d pending team task(s) awaiting dispatch:\n%s\n"+
							"These tasks will be auto-dispatched to available team members. If no longer needed, cancel with team_tasks action=cancel.",
						len(stale), strings.Join(stale, "\n")))
				}
				if len(inProgress) > 0 {
					parts = append(parts, fmt.Sprintf(
						"You have %d in-progress team task(s) being handled by team members:\n%s\n"+
							"Their results will arrive automatically. Do NOT cancel, re-create, or re-spawn these tasks.",
						len(inProgress), strings.Join(inProgress, "\n")))
				}
				if len(parts) > 0 {
					reminder := "[System] " + strings.Join(parts, "\n\n")
					// Merge reminder into the user message as a prefix tag.
					// Previous approach injected [user]+[assistant]+[user] which caused
					// LLMs to treat the assistant ack as "turn complete" → NO_REPLY (#266).
					userMsg := messages[len(messages)-1]
					messages[len(messages)-1] = providers.Message{
						Role:    "user",
						Content: "[Active team tasks]\n" + reminder + "\n[/Active team tasks]\n\n" + userMsg.Content,
					}
				}
			}
		}
	}

	// 2h. Member task reminder: inject task context for members working on dispatched tasks.
	// Caches task subject/number for mid-loop progress nudge (avoids extra DB query).
	var info memberTaskInfo
	if req.TeamTaskID != "" && l.teamStore != nil {
		if taskUUID, err := uuid.Parse(req.TeamTaskID); err == nil {
			if task, err := l.teamStore.GetTask(ctx, taskUUID); err == nil && task != nil {
				info.Subject = task.Subject
				info.TaskNumber = task.TaskNumber
				reminder := fmt.Sprintf(
					"[System] You are working on team task #%d: %q. "+
						"Stay focused on this task. Your final response becomes the task result — make it clear and complete. "+
						"For long tasks, report progress: team_tasks(action=\"progress\", percent=50, text=\"status\").",
					task.TaskNumber, task.Subject)
				// Merge reminder into user message as prefix tag (#266).
				userMsg := messages[len(messages)-1]
				messages[len(messages)-1] = providers.Message{
					Role:    "user",
					Content: "[Task context]\n" + reminder + "\n[/Task context]\n\n" + userMsg.Content,
				}
			}
		}
	}

	return messages, info
}
