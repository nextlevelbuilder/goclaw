package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const maxSubagentsInList = 30

// subagentStatusIcon returns an icon for each subagent task status.
func subagentStatusIcon(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "failed":
		return "❌"
	case "cancelled":
		return "⏹"
	default: // running
		return "🔄"
	}
}

// formatTokenCount formats token counts as "1.2k" for readability.
func formatTokenCount(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// handleSubagentsList handles /subagents — lists subagent tasks from DB.
func (c *Channel) handleSubagentsList(ctx context.Context, chatID int64, isGroup bool, setThread func(*telego.SendMessageParams)) {
	chatIDObj := tu.ID(chatID)

	send := func(text string) {
		msg := tu.Message(chatIDObj, text)
		setThread(msg)
		c.bot.SendMessage(ctx, msg)
	}

	if c.subagentTaskStore == nil {
		send("Subagent task tracking is not available.")
		return
	}

	agentKey := c.AgentID()
	if agentKey == "" {
		send("Subagent tasks are not available (no agent configured).")
		return
	}

	tasks, err := c.subagentTaskStore.ListByParent(ctx, agentKey, "")
	if err != nil {
		slog.Warn("subagents command: ListByParent failed", "error", err)
		send("Failed to list subagent tasks. Please try again.")
		return
	}

	if len(tasks) == 0 {
		send("No subagent tasks found.")
		return
	}

	total := len(tasks)
	if total > maxSubagentsInList {
		tasks = tasks[:maxSubagentsInList]
	}

	var sb strings.Builder
	if total > maxSubagentsInList {
		sb.WriteString(fmt.Sprintf("Subagent tasks (showing %d of %d):\n\n", maxSubagentsInList, total))
	} else {
		sb.WriteString(fmt.Sprintf("Subagent tasks (%d):\n\n", total))
	}

	for i, t := range tasks {
		model := ""
		if t.Model != nil && *t.Model != "" {
			model = *t.Model
		}
		tokens := fmt.Sprintf("%s/%s tokens", formatTokenCount(t.InputTokens), formatTokenCount(t.OutputTokens))
		if model != "" {
			sb.WriteString(fmt.Sprintf("%d. %s %s (%s, %s)\n", i+1, subagentStatusIcon(t.Status), truncateStr(t.Subject, 40), model, tokens))
		} else {
			sb.WriteString(fmt.Sprintf("%d. %s %s (%s)\n", i+1, subagentStatusIcon(t.Status), truncateStr(t.Subject, 40), tokens))
		}
	}
	sb.WriteString("\nTap a button below to view details.")

	var rows [][]telego.InlineKeyboardButton
	for i, t := range tasks {
		label := fmt.Sprintf("%d. %s %s", i+1, subagentStatusIcon(t.Status), truncateStr(t.Subject, 35))
		rows = append(rows, []telego.InlineKeyboardButton{
			{Text: label, CallbackData: "sa:" + t.ID.String()},
		})
	}

	msg := tu.Message(chatIDObj, sb.String())
	setThread(msg)
	if len(rows) > 0 {
		msg.ReplyMarkup = &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	c.bot.SendMessage(ctx, msg)
}

// handleSubagentDetail handles /subagent <id> — shows detail for a subagent task.
func (c *Channel) handleSubagentDetail(ctx context.Context, chatID int64, text string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	chatIDObj := tu.ID(chatID)

	send := func(t string) {
		for _, chunk := range chunkPlainText(t, telegramMaxMessageLen) {
			msg := tu.Message(chatIDObj, chunk)
			setThread(msg)
			c.bot.SendMessage(ctx, msg)
		}
	}

	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		send("Usage: /subagent <task_id>")
		return
	}
	idArg := strings.TrimSpace(parts[1])

	if c.subagentTaskStore == nil {
		send("Subagent task tracking is not available.")
		return
	}

	taskID, err := uuid.Parse(idArg)
	if err != nil {
		send(fmt.Sprintf("Invalid task ID %q. Use /subagents to list tasks.", idArg))
		return
	}

	task, err := c.subagentTaskStore.Get(ctx, taskID)
	if err != nil {
		slog.Warn("subagent command: Get failed", "id", idArg, "error", err)
		send("Failed to load subagent task. Please try again.")
		return
	}
	if task == nil {
		send(fmt.Sprintf("Task %q not found. Use /subagents to see available tasks.", idArg[:8]))
		return
	}

	send(formatSubagentDetail(task))
}

// handleSubagentCallback handles "sa:" callback prefix from inline keyboard buttons.
func (c *Channel) handleSubagentCallback(ctx context.Context, query *telego.CallbackQuery) {
	taskIDStr := strings.TrimPrefix(query.Data, "sa:")

	chat := query.Message.GetChat()
	chatIDObj := tu.ID(chat.ID)

	send := func(text string) {
		for _, chunk := range chunkPlainText(text, telegramMaxMessageLen) {
			msg := tu.Message(chatIDObj, chunk)
			c.bot.SendMessage(ctx, msg)
		}
	}

	if c.subagentTaskStore == nil {
		send("Subagent task tracking is not available.")
		return
	}

	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		send("Invalid task ID.")
		return
	}

	task, err := c.subagentTaskStore.Get(ctx, taskID)
	if err != nil {
		slog.Warn("subagent callback: Get failed", "id", taskIDStr, "error", err)
		send("Failed to load subagent task.")
		return
	}
	if task == nil {
		send(fmt.Sprintf("Task %s not found.", taskIDStr[:8]))
		return
	}

	send(formatSubagentDetail(task))
}

// formatSubagentDetail formats a single subagent task for display.
func formatSubagentDetail(t *store.SubagentTaskData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Subagent: %s\n", t.Subject))
	sb.WriteString(fmt.Sprintf("ID: %s\n", t.ID.String()))
	sb.WriteString(fmt.Sprintf("Status: %s %s\n", subagentStatusIcon(t.Status), t.Status))
	if t.Model != nil && *t.Model != "" {
		sb.WriteString(fmt.Sprintf("Model: %s\n", *t.Model))
	}
	sb.WriteString(fmt.Sprintf("Depth: %d\n", t.Depth))
	sb.WriteString(fmt.Sprintf("Iterations: %d\n", t.Iterations))
	sb.WriteString(fmt.Sprintf("Tokens: %s in / %s out\n", formatTokenCount(t.InputTokens), formatTokenCount(t.OutputTokens)))
	if !t.CreatedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("Created: %s\n", t.CreatedAt.Format("2006-01-02 15:04")))
	}
	if t.Description != "" {
		sb.WriteString(fmt.Sprintf("\nPrompt:\n%s\n", truncateStr(t.Description, 500)))
	}
	if t.Result != nil && *t.Result != "" {
		sb.WriteString(fmt.Sprintf("\nResult:\n%s\n", truncateStr(*t.Result, 1000)))
	}
	return sb.String()
}
