package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- Team tasks ---

const maxTasksInList = 30

// taskStatusIcon returns a short icon for each task status.
func taskStatusIcon(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "in_progress":
		return "🔄"
	case "blocked":
		return "⛔"
	default: // pending
		return "⏳"
	}
}

// truncateStr truncates a string to maxLen runes, appending "…" if truncated.
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// taskUserID composes the scoped user ID for task filtering.
// Groups use "group:{channel}:{chatID}", DMs use the chat ID directly.
func taskUserID(channelName string, chatID int64, isGroup bool) string {
	if isGroup {
		return fmt.Sprintf("group:%s:%d", channelName, chatID)
	}
	return fmt.Sprintf("%d", chatID)
}

// handleTasksList handles the /tasks command — lists team tasks.
func (c *Channel) handleTasksList(ctx context.Context, chatID int64, isGroup bool, setThread func(*telego.SendMessageParams)) {
	chatIDObj := tu.ID(chatID)

	send := func(text string) {
		msg := tu.Message(chatIDObj, text)
		setThread(msg)
		c.bot.SendMessage(ctx, msg)
	}

	if c.teamStore == nil {
		send("Team features are not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("tasks command: agent resolve failed", "error", err)
		send("Team features are not available (no agent).")
		return
	}

	team, err := c.teamStore.GetTeamForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("tasks command: GetTeamForAgent failed", "error", err)
		send("Failed to look up team. Please try again.")
		return
	}
	if team == nil {
		send("This agent is not part of any team.")
		return
	}

	tasks, err := c.teamStore.ListTasks(ctx, team.ID, "newest", store.TeamTaskFilterAll, taskUserID(c.Name(), chatID, isGroup), "", "", 0, 0)
	if err != nil {
		slog.Warn("tasks command: ListTasks failed", "error", err)
		send("Failed to list tasks. Please try again.")
		return
	}

	if len(tasks) == 0 {
		send(fmt.Sprintf("No tasks for team %q.", team.Name))
		return
	}

	total := len(tasks)
	if total > maxTasksInList {
		tasks = tasks[:maxTasksInList]
	}

	var sb strings.Builder
	if total > maxTasksInList {
		sb.WriteString(fmt.Sprintf("Tasks for team %q (showing %d of %d):\n\n", team.Name, maxTasksInList, total))
	} else {
		sb.WriteString(fmt.Sprintf("Tasks for team %q (%d):\n\n", team.Name, total))
	}
	for i, t := range tasks {
		owner := ""
		if t.OwnerAgentKey != "" {
			owner = " — @" + t.OwnerAgentKey
		}
		sb.WriteString(fmt.Sprintf("%d. %s %s%s\n", i+1, taskStatusIcon(t.Status), t.Subject, owner))
	}
	sb.WriteString("\nTap a button below to view details.")

	// Build inline keyboard — one button per task.
	var rows [][]telego.InlineKeyboardButton
	for i, t := range tasks {
		label := fmt.Sprintf("%d. %s %s", i+1, taskStatusIcon(t.Status), truncateStr(t.Subject, 35))
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: label, CallbackData: "td:" + t.ID.String()},
		})
	}

	msg := tu.Message(chatIDObj, sb.String())
	setThread(msg)
	if len(rows) > 0 {
		msg.ReplyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	c.bot.SendMessage(ctx, msg)
}

// handleTaskDetail handles the /task_detail command — shows detail for a task.
func (c *Channel) handleTaskDetail(ctx context.Context, chatID int64, text string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	chatIDObj := tu.ID(chatID)

	send := func(t string) {
		for _, chunk := range chunkPlainText(t, telegramMaxMessageLen) {
			msg := tu.Message(chatIDObj, chunk)
			setThread(msg)
			c.bot.SendMessage(ctx, msg)
		}
	}

	// Extract task ID argument: "/task_detail <id>" or "/task_detail@botname <id>"
	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		send("Usage: /task_detail <task_id>")
		return
	}
	taskIDArg := strings.TrimSpace(parts[1])

	if c.teamStore == nil {
		send("Team features are not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("task_detail command: agent resolve failed", "error", err)
		send("Team features are not available (no agent).")
		return
	}

	team, err := c.teamStore.GetTeamForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("task_detail command: GetTeamForAgent failed", "error", err)
		send("Failed to look up team. Please try again.")
		return
	}
	if team == nil {
		send("This agent is not part of any team.")
		return
	}

	tasks, err := c.teamStore.ListTasks(ctx, team.ID, "newest", store.TeamTaskFilterAll, taskUserID(c.Name(), chatID, isGroup), "", "", 0, 0)
	if err != nil {
		slog.Warn("task_detail command: ListTasks failed", "error", err)
		send("Failed to list tasks. Please try again.")
		return
	}

	// Find task by full UUID or prefix match.
	taskIDLower := strings.ToLower(taskIDArg)
	for i := range tasks {
		tid := tasks[i].ID.String()
		if tid == taskIDLower || strings.HasPrefix(tid, taskIDLower) {
			send(formatTaskDetail(&tasks[i]))
			return
		}
	}

	send(fmt.Sprintf("Task %q not found. Use /tasks to see available tasks.", taskIDArg))
}

// handleCallbackQuery handles inline keyboard button presses.
func (c *Channel) handleCallbackQuery(ctx context.Context, query *telego.CallbackQuery) {
	// Always answer to dismiss the loading indicator.
	c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	})

	if !strings.HasPrefix(query.Data, "td:") {
		return
	}

	taskIDStr := strings.TrimPrefix(query.Data, "td:")

	// Resolve chat ID and group status from the callback's message.
	chat := query.Message.GetChat()
	chatID := chat.ID
	chatIDObj := tu.ID(chatID)
	isGroup := chat.Type == "group" || chat.Type == "supergroup"

	send := func(text string) {
		for _, chunk := range chunkPlainText(text, telegramMaxMessageLen) {
			msg := tu.Message(chatIDObj, chunk)
			c.bot.SendMessage(ctx, msg)
		}
	}

	if c.teamStore == nil {
		send("Team features are not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		send("Team features are not available (no agent).")
		return
	}

	team, err := c.teamStore.GetTeamForAgent(ctx, agentID)
	if err != nil || team == nil {
		send("Could not resolve team.")
		return
	}

	tasks, err := c.teamStore.ListTasks(ctx, team.ID, "newest", store.TeamTaskFilterAll, taskUserID(c.Name(), chatID, isGroup), "", "", 0, 0)
	if err != nil {
		send("Failed to list tasks.")
		return
	}

	for i := range tasks {
		if tasks[i].ID.String() == taskIDStr {
			send(formatTaskDetail(&tasks[i]))
			return
		}
	}
	send(fmt.Sprintf("Task %s not found.", taskIDStr[:8]))
}

// formatTaskDetail formats a single task for display.
func formatTaskDetail(t *store.TeamTaskData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task: %s\n", t.Subject))
	sb.WriteString(fmt.Sprintf("ID: %s\n", t.ID.String()))
	sb.WriteString(fmt.Sprintf("Status: %s %s\n", taskStatusIcon(t.Status), t.Status))
	if t.OwnerAgentKey != "" {
		sb.WriteString(fmt.Sprintf("Owner: @%s\n", t.OwnerAgentKey))
	}
	sb.WriteString(fmt.Sprintf("Priority: %d\n", t.Priority))
	if !t.CreatedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("Created: %s\n", t.CreatedAt.Format("2006-01-02 15:04")))
	}
	if t.Description != "" {
		sb.WriteString(fmt.Sprintf("\nDescription:\n%s\n", t.Description))
	}
	if t.Result != nil && *t.Result != "" {
		sb.WriteString(fmt.Sprintf("\nResult:\n%s\n", *t.Result))
	}
	if len(t.BlockedBy) > 0 {
		ids := make([]string, len(t.BlockedBy))
		for j, bid := range t.BlockedBy {
			ids[j] = bid.String()[:8]
		}
		sb.WriteString(fmt.Sprintf("\nBlocked by: %s\n", strings.Join(ids, ", ")))
	}
	return sb.String()
}
