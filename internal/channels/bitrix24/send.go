package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// rateLimitRetryDelay is how long we wait after Bitrix24 returns
// QUERY_LIMIT_EXCEEDED before retrying. Bitrix's own recommendation is
// 2 seconds; we only retry once per chunk to avoid queueing storms.
const rateLimitRetryDelay = 2 * time.Second

// Send implements channels.Channel by delivering a goclaw OutboundMessage to
// the Bitrix24 portal as one or more imbot.message.add calls.
//
// Contract:
//   - msg.ChatID is a Bitrix DIALOG_ID ("chatNN" for group, numeric for DM).
//     It's passed through verbatim — upstream code already built it from
//     the inbound event's DialogID.
//   - Content is chunked at TextChunkLimit (default 4000) so long LLM
//     responses don't hit Bitrix's 4096-character hard cap.
//   - QUERY_LIMIT_EXCEEDED triggers one 2s retry per chunk (not per
//     message) — rate limits are usually transient.
//   - Media: Phase 06 handles this. Until then we best-effort-log and
//     continue so Phase 03 doesn't silently drop text when media is
//     attached. Do not treat media failures as a Send error.
//
// Returns the first hard (non-rate-limit) error; partial sends surface
// through slog so an operator sees them even when the err path swallows.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return errors.New("bitrix24: channel not running")
	}
	// Liveness-only check — sendChunk re-fetches client/botID under its
	// own lock so we don't hold stale references across the chunk loop.
	if c.Client() == nil || c.BotID() <= 0 {
		return errors.New("bitrix24: channel not initialised")
	}
	if strings.TrimSpace(msg.ChatID) == "" {
		return errors.New("bitrix24: missing chat_id on outbound message")
	}

	// Phase 06 will upload real media here; Phase 03 logs + drops.
	if len(msg.Media) > 0 {
		slog.Info("bitrix24: media attachments present — Phase 06 pending; sending text only",
			"chat_id", msg.ChatID, "count", len(msg.Media))
	}

	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return nil
	}

	// Convert LLM Markdown output to Bitrix24 BBCode BEFORE chunking. The
	// chunker then operates on the final wire shape — whatever it cuts on
	// is what Bitrix24 renders, and we can't leak half-converted Markdown
	// markers (e.g. a lone `**`) to the client. See format.go for the full
	// mapping (bold/italic/code/links/headers/lists/tables).
	//
	// Caveat: the chunker is tag-agnostic. A BBCode pair straddling the
	// 4000-rune boundary can still be split across chunks — Bitrix renders
	// the unclosed tag literally. LLM replies rarely push the limit in
	// practice; if this becomes visible, teach findChunkBoundary to avoid
	// cutting inside [tag] or [tag=…] … [/tag] spans.
	//
	// Idempotency: applying markdownToBitrixBBCode to an already-BBCode
	// string is a no-op — the conversion regexes key off Markdown markers
	// that don't appear in [b]/[i]/[code]/[url=…] syntax.
	text = markdownToBitrixBBCode(text)

	// Prepend an @mention BBCode so multi-user group chats know which user
	// the bot is replying to. Consumer (cmd/gateway_consumer_normal.go) sets
	// the address user_id for group inbounds; DM and synthetic-sender flows
	// leave it empty so this is a no-op there. Prepending BEFORE chunkText
	// guarantees the mention only appears on the first chunk regardless of
	// how the body splits.
	if mention := buildAddressMention(msg.Metadata, c.BotID()); mention != "" {
		text = mention + " " + text
	}

	// TextChunkLimit is always populated by applyConfigDefaults (4000) —
	// chunkText also treats limit<=0 as "use default" as a safety net, so we
	// don't duplicate the fallback here.
	chunks := chunkText(text, c.cfg.TextChunkLimit)
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.sendChunk(ctx, msg.ChatID, chunk); err != nil {
			return fmt.Errorf("bitrix24 send chunk %d/%d: %w", i+1, len(chunks), err)
		}
	}
	return nil
}

// sendChunk posts a single chunk via imbot.message.add. One automatic retry
// on QUERY_LIMIT_EXCEEDED; other errors bubble unchanged.
func (c *Channel) sendChunk(ctx context.Context, chatID, chunk string) error {
	client := c.Client()
	botID := c.BotID()
	if client == nil || botID <= 0 {
		// Channel was shut down between Send's liveness check and here.
		// Report as a transport error so the caller can retry if desired.
		return errors.New("bitrix24: channel lost during send")
	}

	params := map[string]any{
		"BOT_ID":    botID,
		"DIALOG_ID": chatID,
		"MESSAGE":   chunk,
		"SYSTEM":    "N",
	}

	_, err := client.Call(ctx, "imbot.message.add", params)
	if err == nil {
		return nil
	}
	if !isRateLimitErr(err) {
		slog.Warn("bitrix24: imbot.message.add failed",
			"chat_id", chatID, "bot_id", botID, "err", err)
		return err
	}

	// One retry after a short backoff. Use a context-aware sleep so shutdown
	// doesn't hang for 2 seconds.
	slog.Warn("bitrix24: rate limit hit — retrying once",
		"chat_id", chatID, "bot_id", botID)
	select {
	case <-time.After(rateLimitRetryDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	_, err = client.Call(ctx, "imbot.message.add", params)
	return err
}

// buildAddressMention returns the Bitrix24 BBCode @mention prefix for the
// addressee of an outbound message, or "" when no addressee is set or the
// addressee is the bot itself (self-mention guard).
//
// Format is `[USER=<id>][/USER]` — empty inner content. Bitrix renders the
// user's current display name from the id at delivery time, sidestepping
// any escaping concerns with names that contain BBCode metacharacters or
// were renamed since the inbound event was captured.
//
// The metadata key is set by cmd/gateway_consumer_normal.go for group-chat
// outbounds. DM, synthetic-sender, and non-Bitrix channels leave it empty.
func buildAddressMention(meta map[string]string, botID int) string {
	userID := strings.TrimSpace(meta["bitrix_address_user_id"])
	if userID == "" {
		return ""
	}
	// Self-mention guard: bot replying to its own synthetic relay, or a
	// future code path injecting the bot's id by mistake. Don't @mention
	// the bot to itself — Bitrix would render "@Bot Synity" in the bot's
	// own message which is confusing.
	if botID > 0 && userID == strconv.Itoa(botID) {
		return ""
	}
	return "[USER=" + userID + "][/USER]"
}

// isRateLimitErr detects Bitrix24's rate-limit response. The canonical code
// is QUERY_LIMIT_EXCEEDED on the RawResult envelope; net timeouts aren't
// classified here — caller treats them as transport errors.
func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == "QUERY_LIMIT_EXCEEDED" || apiErr.Code == "OPERATION_TIME_LIMIT"
	}
	return false
}

// chunkText splits s into pieces no larger than limit *runes* (not bytes).
// Prefers to break on newline, then whitespace, then hard rune boundary.
// Each returned chunk is a valid UTF-8 string; no trailing whitespace.
//
// The function is intentionally simple — Bitrix24 renders BBCode and
// doesn't need LLM-style sentence-aware splitting. Phase 05 (streaming)
// can layer smarter boundaries on top if prefix flicker becomes a problem.
func chunkText(s string, limit int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if limit <= 0 {
		limit = 4000
	}
	// Count by runes so we don't cut a multi-byte UTF-8 codepoint.
	if utf8.RuneCountInString(s) <= limit {
		return []string{s}
	}

	var out []string
	remaining := s
	for utf8.RuneCountInString(remaining) > limit {
		cut := findChunkBoundary(remaining, limit)
		chunk := strings.TrimRight(remaining[:cut], " \t")
		if chunk == "" {
			// Hard-break fallback: emit the first `limit` runes.
			chunk, remaining = sliceRunes(remaining, limit)
			out = append(out, chunk)
			remaining = strings.TrimLeft(remaining, " \t\r\n")
			continue
		}
		out = append(out, chunk)
		remaining = strings.TrimLeft(remaining[cut:], " \t\r\n")
	}
	if remaining != "" {
		out = append(out, remaining)
	}
	return out
}

// findChunkBoundary returns the byte index in s where we'll cut. Preference
// order: last newline within the first `limit` runes → last whitespace →
// rune boundary at exactly `limit` runes.
func findChunkBoundary(s string, limit int) int {
	// Walk runes until we've counted `limit` of them, tracking last newline
	// and last whitespace offsets as byte indices.
	lastNL := -1
	lastWS := -1
	runes := 0
	for i, r := range s {
		if runes >= limit {
			break
		}
		if r == '\n' {
			lastNL = i
		} else if r == ' ' || r == '\t' {
			lastWS = i
		}
		runes++
	}

	// `>= 0` not `> 0`: a newline / whitespace at byte 0 IS a valid cut point.
	// In practice the outer chunkText TrimSpaces the input and TrimLeft's the
	// remainder every iteration, so byte-0 whitespace "shouldn't" happen — but
	// the `> 0` form silently falls through to the hard-break path when it
	// does, which is the wrong answer. Accept offset 0 so the invariant is
	// expressed here, not only in the caller.
	if lastNL >= 0 {
		return lastNL + 1 // cut AFTER the newline so \n goes in the prior chunk
	}
	if lastWS >= 0 {
		return lastWS + 1
	}

	// Hard break: find the byte offset for rune #limit.
	runes = 0
	for i := range s {
		if runes == limit {
			return i
		}
		runes++
	}
	return len(s)
}

// sliceRunes returns (head, tail) split at exactly `n` runes. head contains
// the first n runes; tail contains the rest. Used as the hard-break fallback
// inside chunkText.
func sliceRunes(s string, n int) (string, string) {
	count := 0
	for i := range s {
		if count == n {
			return s[:i], s[i:]
		}
		count++
	}
	return s, ""
}
