package audio

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// AutoApplyResult holds the result of TTS auto-apply processing.
type AutoApplyResult struct {
	// Text is the message content with [[tts]] directives stripped.
	Text string
	// AudioPath is the path to generated audio file (empty if no TTS applied).
	AudioPath string
	// AudioMime is the MIME type of generated audio (e.g. "audio/ogg").
	AudioMime string
}

// AutoApplyToText checks if TTS should be auto-applied to the message content.
// Returns modified text (directives stripped) and audio path if TTS was applied.
// channel: "telegram", "discord", etc. - used for format selection (opus for telegram).
// isVoiceInbound: true if user sent voice message (for "inbound" auto mode).
// workspace: directory to save generated audio files.
func (m *Manager) AutoApplyToText(
	ctx context.Context,
	content string,
	channel string,
	isVoiceInbound bool,
	workspace string,
) (*AutoApplyResult, error) {
	if m == nil || content == "" {
		return &AutoApplyResult{Text: content}, nil
	}

	// Check if TTS should be applied (respects auto mode: off/always/inbound/tagged)
	result, ok := m.MaybeApply(ctx, content, channel, isVoiceInbound, "final")
	if !ok || result == nil {
		return &AutoApplyResult{Text: StripTTSDirectives(content)}, nil
	}

	// Write audio to workspace/tts/ directory
	ttsDir := workspace
	if ttsDir == "" {
		ttsDir = os.TempDir()
	}
	ttsDir = filepath.Join(ttsDir, "tts")
	if err := os.MkdirAll(ttsDir, 0755); err != nil {
		return &AutoApplyResult{Text: StripTTSDirectives(content)}, err
	}

	audioPath := filepath.Join(ttsDir, "auto-"+time.Now().Format("20060102-150405")+"."+result.Extension)
	if err := os.WriteFile(audioPath, result.Audio, 0644); err != nil {
		return &AutoApplyResult{Text: StripTTSDirectives(content)}, err
	}

	return &AutoApplyResult{
		Text:      StripTTSDirectives(content),
		AudioPath: audioPath,
		AudioMime: result.MimeType,
	}, nil
}
