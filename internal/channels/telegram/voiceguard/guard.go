// Package voiceguard intercepts technical error language in voice agent replies
// and replaces it with user-friendly fallback messages. Pure string transformation
// with zero dependencies on Telegram SDK or message bus.
package voiceguard

import (
	"regexp"
	"strings"
)

// defaultErrorMarkers are patterns that indicate a technical/system error in the reply.
// Matched case-insensitively against the full reply text.
var defaultErrorMarkers = []string{
	// English
	"system error",
	"exit status",
	"rate limit",
	"tool error",
	"service unavailable",
	"technical issue",
	// Vietnamese
	"vấn đề kỹ thuật",
	"lỗi hệ thống",
	"vấn đề hệ thống",
}

const (
	defaultFallbackTranscript   = `I heard you say: "%s". Let me process that — please try again in a moment!`
	defaultFallbackNoTranscript = "I received your voice message but had trouble processing it. Please try again!"
)

// transcriptRe extracts text between <transcript>...</transcript> tags.
var transcriptRe = regexp.MustCompile(`(?s)<transcript>\s*(.*?)\s*</transcript>`)

// SanitizeReply checks whether a voice agent reply contains technical error language
// and replaces it with a user-friendly fallback. Returns the original reply unchanged
// when guard conditions are not met or the reply is clean.
//
// Guard conditions (all must be true):
//   - voiceAgentID is non-empty and matches agentID
//   - channel is "telegram"
//   - peerKind is "direct"
//   - inbound contains <media:voice> or <media:audio>
func SanitizeReply(
	voiceAgentID, agentID, channel, peerKind, inbound, reply string,
	fallbackTranscript, fallbackNoTranscript string,
	errorMarkers []string,
) string {
	// Guard: feature not configured or agent mismatch.
	if voiceAgentID == "" || agentID != voiceAgentID {
		return reply
	}
	// Guard: only Telegram DMs with audio/voice media.
	if channel != "telegram" || peerKind != "direct" {
		return reply
	}
	if !hasAudioTag(inbound) {
		return reply
	}
	// Guard: reply is clean (no technical error language).
	if !containsErrorLanguage(reply, errorMarkers) {
		return reply
	}

	// Build fallback message.
	transcript := extractTranscript(inbound)
	if transcript != "" {
		tpl := fallbackTranscript
		if tpl == "" {
			tpl = defaultFallbackTranscript
		}
		if strings.Contains(tpl, "%s") {
			return strings.Replace(tpl, "%s", transcript, 1)
		}
		// Template has no placeholder — return it as-is to avoid fmt garbage.
		return tpl
	}

	fb := fallbackNoTranscript
	if fb == "" {
		fb = defaultFallbackNoTranscript
	}
	return fb
}

// hasAudioTag checks whether the inbound message contains a voice or audio media tag.
func hasAudioTag(inbound string) bool {
	return strings.Contains(inbound, "<media:voice") || strings.Contains(inbound, "<media:audio")
}

// containsErrorLanguage checks whether the reply contains any error marker.
// When custom markers are provided, they replace the built-in defaults.
func containsErrorLanguage(reply string, customMarkers []string) bool {
	lower := strings.ToLower(reply)
	markers := defaultErrorMarkers
	if len(customMarkers) > 0 {
		markers = customMarkers
	}
	for _, m := range markers {
		if strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// extractTranscript extracts the transcript text from <transcript>...</transcript> tags
// in the inbound message. Returns empty string if no transcript is found.
func extractTranscript(inbound string) string {
	m := transcriptRe.FindStringSubmatch(inbound)
	if len(m) < 2 {
		return ""
	}
	// Collapse internal whitespace/newlines into single spaces.
	return strings.Join(strings.Fields(m[1]), " ")
}
