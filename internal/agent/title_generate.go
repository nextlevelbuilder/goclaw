package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const titleGenerateTimeout = 120 * time.Second

const titleSystemPrompt = `Generate a short title (max 15 words) for this conversation based on the user's message. Reply with only the title, no quotes or punctuation wrapping.`

// GenerateTitle uses a lightweight LLM call to create a short conversation title
// from the user's first message. Returns empty string on error.
func GenerateTitle(ctx context.Context, provider providers.Provider, model, userMessage string) string {
	ctx, cancel := context.WithTimeout(ctx, titleGenerateTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: titleSystemPrompt},
			{Role: "user", Content: userMessage},
		},
		Model: model,
		Options: map[string]any{
			// Larger budget: thinking-capable models (Gemini 2.5/3, GPT-5 reasoning)
			// can consume output tokens on reasoning traces. 256 leaves room for a
			// 15-word title even when the provider allocates some budget to thinking.
			providers.OptMaxTokens:     256,
			providers.OptTemperature:   0.3,
			// Disable extended thinking for title generation — it's a trivial task
			// that doesn't benefit from reasoning and defaults (esp. Gemini's "high")
			// otherwise eat the entire max_tokens budget, truncating the title to 1 word.
			providers.OptThinkingLevel: "off",
		},
	})
	if err != nil {
		slog.Warn("title generation failed", "error", err)
		return ""
	}

	title := strings.TrimSpace(resp.Content)
	// Strip surrounding quotes if present.
	title = strings.Trim(title, "\"'`")
	title = strings.TrimSpace(title)

	if runes := []rune(title); len(runes) > 100 {
		title = string(runes[:100])
	}
	return title
}
