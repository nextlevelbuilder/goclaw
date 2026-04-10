package memory

import "strings"

// maxRecallContextRunes bounds the recent-context snippet used to enrich the
// recall query. Longer snippets dilute the embedding signal and slow down the
// vector search; shorter snippets lose the conversational frame.
//
// 400 runes ≈ 100 tokens in Latin scripts, fewer in CJK (1-2 short user turns
// either way). Tuning knob; change only if recall quality metrics show a
// clear trend in either direction.
//
// Unit is runes, not bytes, because GoClaw supports vi/zh locales: a
// byte-wise tail-clip would slice a multi-byte rune in half and emit invalid
// UTF-8 to the embedding model.
const maxRecallContextRunes = 400

// buildRecallQuery concatenates the user's latest message with a short recent
// context snippet to produce a context-aware search query. Used by auto-inject
// to improve recall on follow-up questions where the current message alone is
// ambiguous (pronouns, implicit references, one-word replies).
//
// The recent context is truncated to maxRecallContextRunes (rune-safe for
// CJK/vi/zh input) and prepended so embedding models give the latest message
// the most weight (position bias). Empty context or empty message return the
// unmodified input — zero-risk fallback for legacy callers that don't supply
// RecentContext yet.
func buildRecallQuery(userMessage, recentContext string) string {
	userMessage = strings.TrimSpace(userMessage)
	if recentContext == "" || userMessage == "" {
		return userMessage
	}

	ctx := strings.TrimSpace(recentContext)
	ctx = tailClipRunes(ctx, maxRecallContextRunes)

	// Prepend context with a lightweight separator. "Context:" and "Query:"
	// tags help instruction-tuned embedding models distinguish frame from
	// focus; for non-instruction models they act as neutral separators.
	var sb strings.Builder
	sb.Grow(len(userMessage) + len(ctx) + 32)
	sb.WriteString("Context: ")
	sb.WriteString(ctx)
	sb.WriteString("\nQuery: ")
	sb.WriteString(userMessage)
	return sb.String()
}

// tailClipRunes returns the last maxRunes runes of s, rune-safe for multi-byte
// scripts (Vietnamese, Chinese, Japanese, emoji). If s has fewer runes than
// maxRunes it's returned unchanged. Byte-wise slicing would split multi-byte
// runes and emit invalid UTF-8.
func tailClipRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[len(runes)-maxRunes:])
}
