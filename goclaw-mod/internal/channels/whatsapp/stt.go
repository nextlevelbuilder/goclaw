package whatsapp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
)

// sttSettings is the minimal contract Phase 5 needs from builtin_tools[stt].settings.
// whatsapp_enabled defaults to false (opt-in) per Decision 6 — admin must explicitly
// enable it, acknowledging that WhatsApp STT breaks E2E encryption for voice messages.
type sttSettings struct {
	WhatsappEnabled bool `json:"whatsapp_enabled"`
}

// loadSTTSettings fetches stt builtin-tool settings and parses whatsapp_enabled.
// Returns opt-out (false) on any failure: nil store, DB error, parse error, or missing key.
// Called per voice message — rare enough that per-message DB read is acceptable.
func (c *Channel) loadSTTSettings(ctx context.Context) sttSettings {
	if c.builtinToolStore == nil {
		return sttSettings{WhatsappEnabled: false}
	}
	raw, err := c.builtinToolStore.GetSettings(ctx, "stt")
	if err != nil {
		slog.Debug("whatsapp: stt settings fetch failed, defaulting to opt-out", "error", err)
		return sttSettings{WhatsappEnabled: false}
	}
	if len(raw) == 0 {
		return sttSettings{WhatsappEnabled: false}
	}
	var s sttSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		slog.Debug("whatsapp: stt settings parse failed, defaulting to opt-out", "error", err)
		return sttSettings{WhatsappEnabled: false}
	}
	return s
}

// transcribeVoice converts a downloaded voice file to text via audio.Manager.
// Returns i18n fallback "[Voice message]" when:
//   - settings.WhatsappEnabled == false (default; opt-in per Decision 6)
//   - audioMgr is nil
//   - Transcribe fails / times out (10s wall clock)
func (c *Channel) transcribeVoice(ctx context.Context, filePath, mimeType, locale string, settings sttSettings) string {
	fallback := i18n.T(locale, i18n.MsgVoiceMessageFallback)
	if !settings.WhatsappEnabled {
		return fallback
	}
	if c.audioMgr == nil {
		return fallback
	}
	sttCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := c.audioMgr.Transcribe(sttCtx, audio.STTInput{FilePath: filePath, MimeType: mimeType}, audio.STTOptions{})
	if err != nil || res == nil {
		slog.Warn("whatsapp: stt failed or timed out", "error", err, "file", filePath)
		return fallback
	}
	slog.Info("audio.stt.whatsapp_invoked", "duration_secs", res.Duration, "lang", res.Language)
	return res.Text
}
