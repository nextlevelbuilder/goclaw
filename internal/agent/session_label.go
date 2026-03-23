package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const labelGenerationPrompt = "Придумай короткое название (3-6 слов) для этого разговора на русском языке. Название должно отражать основную тему. Отвечай ТОЛЬКО названием, без кавычек, точек и лишней пунктуации."

// generateAndSetLabel generates a short Russian label for the session based on the first exchange.
// Runs asynchronously after the first user+assistant turn.
func (l *Loop) generateAndSetLabel(sessionKey, userMsg, assistantMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Truncate messages to avoid wasting tokens
	if utf8.RuneCountInString(userMsg) > 500 {
		runes := []rune(userMsg)
		userMsg = string(runes[:500]) + "..."
	}
	if utf8.RuneCountInString(assistantMsg) > 500 {
		runes := []rune(assistantMsg)
		assistantMsg = string(runes[:500]) + "..."
	}

	messages := []providers.Message{
		{Role: "user", Content: userMsg},
		{Role: "assistant", Content: assistantMsg},
		{Role: "user", Content: labelGenerationPrompt},
	}

	resp, err := l.provider.Chat(ctx, providers.ChatRequest{
		Messages: messages,
		Model:    l.model,
		Options: map[string]any{
			"max_tokens":  30,
			"temperature": 0.3,
		},
	})
	if err != nil {
		slog.Warn("session label: LLM call failed", "session", sessionKey, "error", err)
		return
	}

	label := strings.TrimSpace(resp.Content)
	if label == "" {
		return
	}

	l.sessions.SetLabel(context.Background(), sessionKey, label)
	if err := l.sessions.Save(context.Background(), sessionKey); err != nil {
		slog.Warn("session label: save failed", "session", sessionKey, "error", err)
	}
	slog.Info("session label: generated", "session", sessionKey, "label", label)
}
