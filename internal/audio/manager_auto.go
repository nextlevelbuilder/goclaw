package audio

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	// Try tenant-specific TTS config first
	tenantProvider, _, tenantAuto, hasTenant := m.ResolveTenantProvider(ctx)

	auto := m.auto
	if hasTenant && tenantAuto != "" {
		auto = tenantAuto
	}

	if auto == AutoOff {
		return nil, false
	}

	// Mode filter: ModeFinal skips tool/block replies.
	if m.mode == ModeFinal && (kind == "tool" || kind == "block") {
		return nil, false
	}

	switch auto {
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
	cleanText = StripTTSDirectives(cleanText)
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

	// Apply per-agent voice/model override from context (set by dispatch.go from OutboundMessage)
	if snap, ok := store.AgentAudioFromCtx(ctx); ok && len(snap.OtherConfig) > 0 {
		var agentCfg struct {
			TTSVoiceID string `json:"tts_voice_id,omitempty"`
			TTSModelID string `json:"tts_model_id,omitempty"`
		}
		if err := json.Unmarshal(snap.OtherConfig, &agentCfg); err == nil {
			if agentCfg.TTSVoiceID != "" {
				opts.Voice = agentCfg.TTSVoiceID
			}
			if agentCfg.TTSModelID != "" {
				opts.Model = agentCfg.TTSModelID
			}
		}
	}

	var result *SynthResult
	var err error

	// Use tenant provider if available, otherwise fall back to global
	if hasTenant && tenantProvider != nil {
		result, err = tenantProvider.Synthesize(ctx, cleanText, opts)
	} else {
		result, err = m.SynthesizeWithFallback(ctx, cleanText, opts)
	}

	if err != nil {
		slog.Warn("tts auto-apply failed", "error", err)
		return nil, false
	}
	return result, true
}
