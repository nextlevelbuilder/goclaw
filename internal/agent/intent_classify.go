package agent

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// IntentType represents the classified intent of a user message.
type IntentType string

const (
	IntentStatusQuery IntentType = "status_query"
	IntentCancel      IntentType = "cancel"
	IntentSteer       IntentType = "steer"
	IntentNewTask     IntentType = "new_task"
)

const intentClassifyTimeout = 10 * time.Second

const intentSystemPrompt = `You are an intent classifier. The user has sent a message while the AI assistant is busy processing a previous request.

Classify the user's intent into exactly ONE of these categories:
- status_query: The user is asking about progress, status, or what the assistant is currently doing (e.g., "what are you doing?", "status?", "how far along?", "đang làm gì?", "bao giờ xong?")
- cancel: The user wants to stop or cancel the current task (e.g., "stop", "cancel", "nevermind", "thôi", "dừng lại")
- steer: The user wants to add instructions or redirect the current task (e.g., "also check X", "focus on Y instead", "thêm phần Z nữa")
- new_task: The user is sending a new unrelated request or message

Respond with ONLY the category name, nothing else.`

// cancelKeywords for fast-path detection of obvious cancel intents.
// Only matched on very short messages (≤ 15 runes) to avoid false positives
// like "làm đơn giản thôi" matching "thôi".
var cancelKeywords = []string{
	"stop", "cancel", "abort", "thôi", "dừng", "hủy", "取消", "停",
	"nevermind", "never mind",
}

// quickClassify attempts keyword-based classification for ultra-short messages
// before calling the LLM. Only messages ≤ 15 runes are fast-pathed; longer
// messages always go to LLM for proper context understanding.
// Cancel keywords require a whole-word match to avoid false positives (e.g. "nonstop").
// Returns (intent, true) if a match is found, or ("", false) to fall through to LLM.
func quickClassify(msg string) (IntentType, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if utf8.RuneCountInString(lower) > 15 {
		return "", false
	}
	// Exact "?" → status query
	if lower == "?" {
		return IntentStatusQuery, true
	}
	for _, kw := range cancelKeywords {
		if containsWholeWord(lower, kw) {
			return IntentCancel, true
		}
	}
	return "", false
}

// containsWholeWord checks if s contains kw as a whole word (not a substring
// of a larger word). Word boundaries are: start/end of string, spaces, punctuation.
func containsWholeWord(s, kw string) bool {
	idx := strings.Index(s, kw)
	if idx < 0 {
		return false
	}
	// Check left boundary: must be start of string or non-letter
	if idx > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:idx])
		if isWordChar(r) {
			return false
		}
	}
	// Check right boundary: must be end of string or non-letter
	end := idx + len(kw)
	if end < len(s) {
		r, _ := utf8.DecodeRuneInString(s[end:])
		if isWordChar(r) {
			return false
		}
	}
	return true
}

// ClassifyIntent determines the intent of a user message sent while the agent is busy.
// Uses keyword fast-path first, then falls back to LLM classification.
// Falls back to IntentNewTask on any error.
func ClassifyIntent(ctx context.Context, provider providers.Provider, model, userMessage string) IntentType {
	// Fast-path: keyword matching for obvious patterns (no LLM cost).
	if intent, ok := quickClassify(userMessage); ok {
		return intent
	}

	ctx, cancel := context.WithTimeout(ctx, intentClassifyTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: intentSystemPrompt},
			{Role: "user", Content: userMessage},
		},
		Model: model,
		Options: map[string]any{
			providers.OptMaxTokens:   20,
			providers.OptTemperature: 0.0,
		},
	})
	if err != nil {
		return IntentNewTask
	}

	result := strings.TrimSpace(strings.ToLower(resp.Content))
	switch {
	case strings.Contains(result, "status_query"):
		return IntentStatusQuery
	case strings.Contains(result, "cancel"):
		return IntentCancel
	case strings.Contains(result, "steer"):
		return IntentSteer
	default:
		return IntentNewTask
	}
}

// FormatStatusReply builds a user-friendly status response from the current agent activity.
func FormatStatusReply(status *AgentActivityStatus, locale string) string {
	if status == nil {
		return i18n.T(locale, i18n.MsgStatusWorking)
	}

	elapsed := time.Since(status.StartedAt).Round(time.Second)
	phase := formatPhase(status.Phase, status.Tool, locale)

	return i18n.T(locale, i18n.MsgStatusDetailed, phase, status.Iteration, elapsed)
}

// formatPhase returns a human-readable phase description.
func formatPhase(phase, tool, locale string) string {
	switch phase {
	case "thinking":
		return i18n.T(locale, i18n.MsgStatusPhaseThinking)
	case "tool_exec":
		if tool != "" {
			return i18n.T(locale, i18n.MsgStatusPhaseToolExec, formatToolLabel(tool))
		}
		return i18n.T(locale, i18n.MsgStatusPhaseTools)
	case "compacting":
		return i18n.T(locale, i18n.MsgStatusPhaseCompact)
	default:
		return i18n.T(locale, i18n.MsgStatusPhaseDefault)
	}
}

// formatToolLabel returns a user-friendly label for a tool name.
func formatToolLabel(tool string) string {
	switch {
	case strings.HasPrefix(tool, "web"):
		return "web search"
	case tool == "exec":
		return "code execution"
	case tool == "browser":
		return "browser"
	case tool == "spawn":
		return "delegation"
	case strings.HasPrefix(tool, "memory"):
		return "memory"
	case strings.HasPrefix(tool, "file"):
		return "file operations"
	default:
		return tool
	}
}
