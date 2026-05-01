package channels

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// voiceSummaryPrompt is the system prompt for the voice-session
// summarizer. Keeps the output short and Discord-friendly.
const voiceSummaryPrompt = `You are summarizing a Discord voice channel conversation for a transcript channel.

The user will provide a transcript with each line in the form "<speaker name>: <what they said>". Your job is to produce a tight summary suitable for a Discord channel message: 2-4 short paragraphs OR a 3-6 bullet list, whichever fits the conversation better.

Guidelines:
- Lead with the topic or decision, not generic preamble.
- Mention speakers by name when attribution matters; omit names for filler.
- Quote at most one short, distinctive line if it's load-bearing.
- Skip filler ("uh", "you know"), greetings, and side-channel chatter.
- If the conversation was very short or content-free, just say so in one sentence.
- Use plain text — no Markdown headings; small inline emphasis is fine.

Stay under 1500 characters total.`

// BuildVoiceTranscriptSummarizer wraps a VoiceTranscriptSummarizerConfig
// into a closure suitable for voice.Config.TranscriptSummarizer. The
// returned function calls the configured provider's Chat endpoint with
// the system prompt above and the transcript as the user message,
// returning the model's reply (trimmed).
//
// Returns nil if cfg is nil or has no Provider — callers should treat
// nil as "no summarizer wired" and fall back to the legacy stats line.
func BuildVoiceTranscriptSummarizer(cfg *VoiceTranscriptSummarizerConfig) func(ctx context.Context, transcript string) (string, error) {
	if cfg == nil || cfg.Provider == nil || cfg.Model == "" {
		return nil
	}
	maxTokens := cfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}
	provider := cfg.Provider
	model := cfg.Model

	return func(ctx context.Context, transcript string) (string, error) {
		t := strings.TrimSpace(transcript)
		if t == "" {
			return "", errors.New("voice summarizer: empty transcript")
		}
		// Note on temperature: gpt-5 (and the o-series reasoning models)
		// reject explicit temperature values other than 1, returning
		// HTTP 400 "Unsupported value: 'temperature' does not support
		// 0.4 with this model." Other providers/models accept
		// temperature freely. Omit the parameter entirely so each
		// provider gets its own default — the summarization prompt is
		// constrained enough that a 1.0 sample is fine for our use.
		resp, err := provider.Chat(ctx, providers.ChatRequest{
			Messages: []providers.Message{
				{Role: "system", Content: voiceSummaryPrompt},
				{Role: "user", Content: t},
			},
			Model:   model,
			Options: map[string]any{"max_tokens": maxTokens},
		})
		if err != nil {
			return "", fmt.Errorf("voice summarizer chat: %w", err)
		}
		return strings.TrimSpace(resp.Content), nil
	}
}
