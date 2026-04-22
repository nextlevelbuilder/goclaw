package audio

import (
	"context"
	"log/slog"
	"strings"
)

// MaybeApply inspects auto-mode and conditionally applies TTS to a reply.
// Returns (result, true) on success, (nil, false) when auto is disabled, the
// reply type is filtered out, content fails validation, or synthesis fails.
//
// Parameters:
//   - text: the reply text to potentially convert
//   - channel: origin channel ("telegram" switches format to opus)
//   - isVoiceInbound: whether the user's inbound message was voice
//   - kind: "tool", "block", or "final"
func (m *Manager) MaybeApply(ctx context.Context, text, channel string, isVoiceInbound bool, kind string) (*SynthResult, bool) {
	if m.auto == AutoOff {
		return nil, false
	}

	// Mode filter: ModeFinal skips tool/block replies.
	if m.mode == ModeFinal && (kind == "tool" || kind == "block") {
		return nil, false
	}

	switch m.auto {
	case AutoInbound:
		if !isVoiceInbound {
			return nil, false
		}
	case AutoTagged:
		if !strings.Contains(text, "[[tts]]") && !strings.Contains(text, "[[tts:") {
			return nil, false
		}
	case AutoAlways:
		// Always apply.
	default:
		return nil, false
	}

	// Content validation (matches legacy TTS behavior).
	cleanText := stripMarkdown(text)
	cleanText = stripTtsDirectives(cleanText)
	cleanText = strings.TrimSpace(cleanText)

	if len(cleanText) < 10 {
		return nil, false
	}
	if strings.Contains(cleanText, "MEDIA:") {
		return nil, false
	}

	if len(cleanText) > m.maxLength {
		cleanText = cleanText[:m.maxLength] + "..."
	}

	opts := TTSOptions{}
	if channel == "telegram" {
		opts.Format = "opus" // Telegram voice bubbles need opus
	}

	result, err := m.SynthesizeWithFallback(ctx, cleanText, opts)
	if err != nil {
		slog.Warn("tts auto-apply failed", "error", err)
		return nil, false
	}
	return result, true
}
