package bitrix24

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// mentionMatcher is the compiled-once regex + rendered tag string used to
// detect whether a group message @-tags this bot. Cached on the Channel
// and invalidated automatically when the channel's bot_id changes.
type mentionMatcher struct {
	// botID is what this matcher was compiled for. Used to detect staleness
	// after a Reload() that re-registered with a different imbot id.
	botID int
	// stripRe matches `[USER=<id>] ... [/USER]` (or BOT= variant) so we can
	// remove the mention before passing to the agent. Scoped to THIS bot_id
	// — mentions of other users/bots stay intact.
	stripRe *regexp.Regexp
	// tags are the literal `[USER=123]` / `[BOT=123]` openers we look for
	// in the fast-path Contains check (regexp alloc avoided on happy path).
	tags []string
}

// DispatchEvent implements BotDispatcher. Called from Router.handleEvent in
// its own goroutine — we still return quickly (no synchronous Bitrix call
// back to the portal) so the router can move on to the next webhook.
//
// Events are dispatched by type:
//   - ONIMBOTMESSAGEADD → handleMessage (policy + HandleMessage → bus)
//   - ONIMBOTJOINCHAT   → handleJoin    (send welcome)
//   - ONIMBOTDELETE     → unregister bot and mark health stopped
//
// Unknown event types are logged at Info so we notice new Bitrix24 payloads
// without spamming the error log.
func (c *Channel) DispatchEvent(ctx context.Context, evt *Event) {
	if evt == nil {
		return
	}
	switch evt.Type {
	case EventMessageAdd:
		c.handleMessage(ctx, evt)
	case EventJoinChat:
		c.handleJoin(ctx, evt)
	case EventBotDelete:
		// Defense in depth: only teardown when the delete is for OUR bot. Router
		// shouldn't dispatch a mismatched event here, but if it ever does, don't
		// unregister someone else's entry by mistake. Snapshot the bot id once
		// so the compare + log don't re-acquire startMu.
		ourBotID := c.BotID()
		if evt.Params.BotID != ourBotID {
			slog.Warn("bitrix24: ONIMBOTDELETE for a different bot id — ignoring",
				"event_bot_id", evt.Params.BotID, "channel_bot_id", ourBotID,
				"portal", c.cfg.Portal)
			return
		}
		// Reuse Stop() so the teardown path matches channel-shutdown exactly
		// (Router unregister + SetRunning(false) + MarkStopped("") + close(stopCh)).
		// Stop() records the generic "Stopped" summary; immediately overwrite it
		// with the ONIMBOTDELETE-specific reason so operators viewing channel
		// health can distinguish "user deleted our bot on the portal" from a
		// normal shutdown. Stop() currently can't return an error — ignored on
		// purpose; if that changes we'll need to decide whether teardown errors
		// should block the health-override or propagate.
		_ = c.Stop(ctx)
		c.MarkStopped("Bot deleted on portal")
	case EventMessageUpdate, EventMessageDelete:
		// Phase 03 scope: ignore edits/deletes. Phase 05 may surface them to
		// the agent for context pruning.
		slog.Debug("bitrix24: ignoring message edit/delete event",
			"event", evt.Type, "bot_id", evt.Params.BotID, "portal", c.cfg.Portal)
	default:
		slog.Info("bitrix24: unhandled event type",
			"event", evt.Type, "bot_id", evt.Params.BotID, "portal", c.cfg.Portal)
	}
}

// handleMessage turns an ONIMBOTMESSAGEADD into a bus.InboundMessage.
//
// Control flow (each step returns early on deny/drop):
//  1. Classify peer kind (DM vs group) from MESSAGE_TYPE.
//  2. Group-only: require-mention gate + strip the mention from the text.
//  3. Skip empty payloads (no text + no media).
//  4. Policy: DM vs Group via BaseChannel.CheckDMPolicy / CheckGroupPolicy.
//     Pairing policies trigger a pairing-reply stub (Phase 07 will wire up
//     the full pairing flow; Phase 03 logs and drops).
//  5. Build metadata map with bitrix_* keys so the agent can echo / reply
//     to the right dialog + message ID later.
//  6. Forward to BaseChannel.HandleMessage → publishes bus.InboundMessage.
func (c *Channel) handleMessage(ctx context.Context, evt *Event) {
	if evt.Params.FromUserID == "" {
		return // malformed event; router already logged if this matters
	}

	// System messages (e.g. "user X joined the chat") should not trigger
	// agent replies. Bitrix flags these with SYSTEM=Y.
	if evt.Params.SystemMessage {
		return
	}

	isGroup := isGroupMessageType(evt.Params.MessageType)
	text := evt.Params.Message
	if isGroup {
		mentioned := c.isMentioned(text)
		if c.RequireMention() && !mentioned {
			return
		}
		text = c.stripMention(text)
	}
	text = strings.TrimSpace(text)
	if text == "" && len(evt.Params.Files) == 0 {
		return
	}

	senderID := evt.Params.FromUserID
	chatID := evt.Params.DialogID
	peerKind := "direct"
	if isGroup {
		peerKind = "group"
	}

	// Policy gating. BaseChannel reports PolicyNeedsPairing for unpaired DMs
	// under the default "pairing" policy. Phase 07 will answer with a real
	// pairing reply; Phase 03 logs the intent so the flow is visible while
	// the rest of the channel comes online.
	if peerKind == "direct" {
		switch c.CheckDMPolicy(ctx, senderID, c.cfg.DMPolicy) {
		case channels.PolicyDeny:
			return
		case channels.PolicyNeedsPairing:
			c.logPairingNeeded(senderID, chatID, peerKind)
			return
		}
	} else {
		switch c.CheckGroupPolicy(ctx, senderID, chatID, c.cfg.GroupPolicy) {
		case channels.PolicyDeny:
			return
		case channels.PolicyNeedsPairing:
			c.logPairingNeeded(senderID, chatID, peerKind)
			return
		}
	}

	meta := map[string]string{
		"bitrix_dialog_id":  evt.Params.DialogID,
		"bitrix_portal":     c.portalDomainSafe(),
		"bitrix_bot_id":     strconv.Itoa(c.BotID()),
		"bitrix_bot_code":   c.cfg.BotCode,
		"bitrix_message_id": evt.Params.MessageID,
	}
	if evt.Params.ReplyToMID != "" {
		meta["bitrix_reply_to_mid"] = evt.Params.ReplyToMID
	}
	if evt.Params.ChatID != "" {
		meta["bitrix_chat_id"] = evt.Params.ChatID
	}

	// Phase 06 will populate media paths after downloading from disk.getExternalLink;
	// Phase 03 passes an empty slice so text-only flow is correct end-to-end.
	var media []string
	c.HandleMessage(senderID, chatID, text, media, meta, peerKind)
}

// handleJoin sends a short welcome the first time the bot is added to a
// chat. Failure is non-fatal — the agent will still respond to the user's
// first real message.
func (c *Channel) handleJoin(ctx context.Context, evt *Event) {
	client := c.Client()
	botID := c.BotID()
	if client == nil || botID <= 0 {
		return
	}
	if strings.TrimSpace(evt.Params.DialogID) == "" {
		return
	}
	welcome := fmt.Sprintf("Xin chào! Tôi là %s. Hãy hỏi tôi bất cứ điều gì.", c.cfg.BotName)
	if _, err := client.Call(ctx, "imbot.message.add", map[string]any{
		"BOT_ID":    botID,
		"DIALOG_ID": evt.Params.DialogID,
		"MESSAGE":   welcome,
		"SYSTEM":    "N",
	}); err != nil {
		slog.Warn("bitrix24: welcome message send failed",
			"dialog_id", evt.Params.DialogID, "err", err)
	}
}

// isMentioned returns true when the message contains a [USER=<bot_id>] or
// [BOT=<bot_id>] tag matching this channel's bot. The fast path is a plain
// substring check; the regex is only built lazily for stripMention.
func (c *Channel) isMentioned(msg string) bool {
	m := c.mention()
	if m == nil {
		return false
	}
	for _, tag := range m.tags {
		if strings.Contains(msg, tag) {
			return true
		}
	}
	return false
}

// stripMention removes all [USER=<bot_id>]…[/USER] / [BOT=<bot_id>]…[/BOT]
// fragments belonging to this bot, leaving other mentions intact. The result
// may have leading/trailing whitespace — trimming is the caller's job
// (handleMessage already TrimSpaces both DM and group paths uniformly).
func (c *Channel) stripMention(msg string) string {
	m := c.mention()
	if m == nil || m.stripRe == nil {
		return msg
	}
	return m.stripRe.ReplaceAllString(msg, "")
}

// mention returns the cached matcher for this bot's id, building it lazily
// on first use and rebuilding it when bot_id changes.
//
// Returns nil when the bot id isn't resolved yet (pre-Start) or when regex
// compilation fails (logged once — next call will retry).
//
// Uses its own mutex (mentionMu) rather than piggybacking on startMu because
// the hot read path runs on every group message and we don't want it to
// contend with Start/Stop's long-held lock.
func (c *Channel) mention() *mentionMatcher {
	botID := c.BotID()
	if botID <= 0 {
		return nil
	}
	c.mentionMu.Lock()
	defer c.mentionMu.Unlock()
	if c.mentionRe != nil && c.mentionRe.botID == botID {
		return c.mentionRe
	}
	id := strconv.Itoa(botID)
	// Tolerant regex: closing tag may be [/USER] or [/BOT] regardless of
	// the opener variant (some Bitrix clients mismatch).
	//
	// Body uses non-greedy `.*?` (with `(?s)` so `.` matches \n) so a mention
	// whose display text contains nested BBCode — e.g. `[USER=101][b]Boss[/b][/USER]`
	// — still matches. The earlier `[^\[]*` form stopped at the first `[` of
	// the nested tag and never reached `[/USER]`, leaving raw BBCode in the
	// prompt. Non-greedy is safe even across multiple mentions of this bot
	// in one message because each iteration starts at the next `[USER=ID]`.
	pattern := fmt.Sprintf(`(?s)\[(USER|BOT)=%s\].*?\[/(?:USER|BOT)\]`, id)
	re, err := regexp.Compile(pattern)
	if err != nil {
		slog.Warn("bitrix24: failed to compile mention regex",
			"bot_id", id, "err", err)
		return nil
	}
	c.mentionRe = &mentionMatcher{
		botID:   botID,
		stripRe: re,
		tags:    []string{"[USER=" + id + "]", "[BOT=" + id + "]"},
	}
	return c.mentionRe
}

// logPairingNeeded is the Phase 03 stand-in for a real pairing reply.
// Phase 07 replaces this with a proper pairing message via imbot.message.add;
// until then we log the intent so operators can see unpaired attempts.
func (c *Channel) logPairingNeeded(senderID, chatID, peerKind string) {
	if !c.CanSendPairingNotif(senderID, pairingDebounce) {
		return
	}
	c.MarkPairingNotifSent(senderID)
	slog.Info("bitrix24: pairing required (Phase 07 will send pairing reply)",
		"sender_id", senderID, "chat_id", chatID, "peer_kind", peerKind,
		"portal", c.cfg.Portal)
}

// portalDomainSafe reads the portal domain under the start lock. Returns
// empty string if the channel hasn't finished starting yet — callers treat
// that as "unknown portal" and the session router will still work off
// tenant + bot_code.
func (c *Channel) portalDomainSafe() string {
	p := c.Portal()
	if p == nil {
		return ""
	}
	return p.Domain()
}

// pairingDebounce throttles the logPairingNeeded warnings so a spammy
// sender can't flood the log. 60s matches the Telegram channel convention.
const pairingDebounce = 60 * time.Second

// isGroupMessageType normalises Bitrix24 MESSAGE_TYPE to group-or-not.
// Webhook events use short codes ("P" private, "C" chat, "O" open channel)
// while some legacy payloads spell the full word. We accept both; anything
// else (including the empty string) is treated as a direct message so
// stricter DM policies apply.
func isGroupMessageType(mt string) bool {
	switch strings.ToUpper(strings.TrimSpace(mt)) {
	case "C", "CHAT", "O", "OPEN":
		return true
	default:
		return false
	}
}
