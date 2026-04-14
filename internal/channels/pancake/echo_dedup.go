package pancake

import (
	"html"
	"regexp"
	"strings"
	"time"
)

var (
	htmlBreakTagRe = regexp.MustCompile(`(?i)<br\b[^>]*\/?>`)
	htmlCloseTagRe = regexp.MustCompile(`(?i)</(?:div|p|li|ul|ol|h[1-6])>`)
	htmlTagRe      = regexp.MustCompile(`(?i)<[^>]+>`)
)

// isDup checks and records a dedup key. Returns true if the key was already seen.
func (ch *Channel) isDup(key string) bool {
	_, loaded := ch.dedup.LoadOrStore(key, time.Now())
	return loaded
}

// splitMessage splits text into chunks no longer than maxLen Unicode code points.
// Operates on runes (not bytes) to avoid splitting multi-byte characters (CJK, emoji, Vietnamese).
func splitMessage(text string, maxLen int) []string {
	runes := []rune(text)
	if maxLen <= 0 || len(runes) <= maxLen {
		return []string{text}
	}
	var parts []string
	for len(runes) > maxLen {
		parts = append(parts, string(runes[:maxLen]))
		runes = runes[maxLen:]
	}
	if len(runes) > 0 {
		parts = append(parts, string(runes))
	}
	return parts
}

func (ch *Channel) forgetOutboundEcho(conversationID, content string) {
	if conversationID == "" {
		return
	}
	normalized := normalizeEchoContent(content)
	if normalized == "" {
		return
	}
	ch.recentOutbound.Delete(conversationID + "\x00" + normalized)
}

func (ch *Channel) rememberOutboundEcho(conversationID, content string) {
	if conversationID == "" {
		return
	}
	normalized := normalizeEchoContent(content)
	if normalized == "" {
		return
	}
	ch.recentOutbound.Store(conversationID+"\x00"+normalized, time.Now())
}

func (ch *Channel) isRecentOutboundEcho(conversationID, content string) bool {
	if conversationID == "" {
		return false
	}
	normalized := normalizeEchoContent(content)
	if normalized == "" {
		return false
	}
	key := conversationID + "\x00" + normalized
	v, ok := ch.recentOutbound.Load(key)
	if !ok {
		return false
	}
	ts, ok := v.(time.Time)
	if !ok {
		ch.recentOutbound.Delete(key)
		return false
	}
	if time.Since(ts) > outboundEchoTTL {
		ch.recentOutbound.Delete(key)
		return false
	}
	return true
}

func normalizeEchoContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	content = html.UnescapeString(content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = htmlBreakTagRe.ReplaceAllString(content, "\n")
	content = htmlCloseTagRe.ReplaceAllString(content, "\n")
	content = htmlTagRe.ReplaceAllString(content, "")

	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, len(lines))
	pendingBlank := false
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(normalized) == 0 || pendingBlank {
				continue
			}
			pendingBlank = true
			continue
		}
		if pendingBlank {
			normalized = append(normalized, "")
			pendingBlank = false
		}
		normalized = append(normalized, line)
	}

	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

// firstInboxSentTTL controls how long a senderID is retained in firstInboxSent.
// After this period, the sender can receive the first-inbox DM again (e.g. new session after a long gap).
const firstInboxSentTTL = 72 * time.Hour

// runDedupCleaner evicts dedup entries older than dedupTTL every dedupCleanEvery.
// Also evicts firstInboxSent entries to bound memory growth on high-traffic pages.
func (ch *Channel) runDedupCleaner() {
	ticker := time.NewTicker(dedupCleanEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ch.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			ch.dedup.Range(func(k, v any) bool {
				if t, ok := v.(time.Time); ok && now.Sub(t) > dedupTTL {
					ch.dedup.Delete(k)
				}
				return true
			})
			ch.recentOutbound.Range(func(k, v any) bool {
				if t, ok := v.(time.Time); ok && now.Sub(t) > outboundEchoTTL {
					ch.recentOutbound.Delete(k)
				}
				return true
			})
			ch.firstInboxSent.Range(func(k, v any) bool {
				if t, ok := v.(time.Time); ok && now.Sub(t) > firstInboxSentTTL {
					ch.firstInboxSent.Delete(k)
				}
				return true
			})
		}
	}
}
