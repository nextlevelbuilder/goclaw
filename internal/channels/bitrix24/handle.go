package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	// Inject tenant scope so store queries filter by the correct tenant_id.
	ctx = store.WithTenantID(ctx, c.TenantID())

	if evt.Params.FromUserID == "" {
		return // malformed event; router already logged if this matters
	}

	// System messages (e.g. "user X joined the chat") should not trigger
	// agent replies. Bitrix flags these with SYSTEM=Y.
	if evt.Params.SystemMessage {
		return
	}

	// Open Channel (Bitrix24 Lines) carries MESSAGE_TYPE="L" and
	// CHAT_ENTITY_TYPE="LINES". The session mixes real customers coming in
	// through a connector (Zalo/FB/...) with internal staff who join to
	// supervise. Customers can't @-mention the bot from outside; treating the
	// session as a generic group chat would either spam customers (drop when
	// require_mention is true, reply to everything when false) or silently
	// merge into the "direct" path. Recognise it up-front so the gate below
	// can apply the right policy.
	isOpenChannel := strings.EqualFold(evt.Params.MessageType, "L") ||
		strings.EqualFold(evt.Params.ChatEntityType, "LINES")
	// Force group routing for Open Channel so the existing mention-strip /
	// readable-mention pipeline below runs, and so the session key includes
	// the chat id instead of dumping every participant into the "direct"
	// bucket.
	isGroup := isGroupMessageType(evt.Params.MessageType) || isOpenChannel
	text := evt.Params.Message
	slog.Info("bitrix24 message: handle entry",
		"from_user_id", evt.Params.FromUserID,
		"dialog_id", evt.Params.DialogID,
		"message_type", evt.Params.MessageType,
		"is_group", isGroup,
		"is_open_channel", isOpenChannel,
		"from_connector", evt.Params.FromIsConnector,
		"require_mention", c.RequireMention(),
		"message_id", evt.Params.MessageID,
		"mentioned_list_n", len(evt.Params.MentionedList),
	)
	// Open Channel gate (must run BEFORE the generic group block):
	// Mention is the only criterion — both internal staff and external
	// customers (IS_CONNECTOR=Y) can trigger the bot when they @-mention
	// it. The connector side relies on upstream populating MENTIONED_LIST
	// with the bot id when the customer addresses the bot from Zalo/FB;
	// without an explicit mention, traffic is dropped so the bot doesn't
	// spam the customer or interfere with operator handling.
	if isOpenChannel {
		if !c.isMentionedParams(&evt.Params) {
			slog.Info("bitrix24 message: dropped OL message without mention",
				"from_user_id", evt.Params.FromUserID,
				"from_connector", evt.Params.FromIsConnector,
				"dialog_id", evt.Params.DialogID,
				"message_id", evt.Params.MessageID)
			return
		}
	}
	if isGroup {
		// Authority-ordered fallback: structured MENTIONED_LIST → raw
		// MESSAGE_ORIGINAL → stripped MESSAGE. In group chats Bitrix24 strips
		// the @mention from MESSAGE before sending the webhook, so checking
		// MESSAGE alone misses every group mention. See
		// plans/bitrix24-mcp-refactor/reports/retrospective.md §2 for context.
		mentioned := c.isMentionedParams(&evt.Params)
		if c.RequireMention() && !mentioned {
			slog.Info("bitrix24 message: dropped missing mention",
				"from_user_id", evt.Params.FromUserID,
				"dialog_id", evt.Params.DialogID,
				"message_type", evt.Params.MessageType,
				"message_id", evt.Params.MessageID,
			)
			return
		}
		// Prefer MESSAGE_ORIGINAL (raw BBCode) over MESSAGE for groups: Bitrix24
		// strips ALL `[USER=<id>]…[/USER]` mentions — including mentions of OTHER
		// users — from MESSAGE before sending the webhook. Without this, a
		// message like "[USER=982]Alice[/USER] [USER=62]Bob[/USER] help us"
		// reaches the agent as just "help us", losing the addressed-user context.
		//
		// Pipeline: stripMention removes THIS bot's own tag → convert remaining
		// user/bot mentions to "@Name (ID:<id>)" so the LLM sees who else was
		// addressed without parsing BBCode. Falls back to MESSAGE on legacy
		// portals that don't ship MESSAGE_ORIGINAL.
		if evt.Params.MessageOriginal != "" {
			text = evt.Params.MessageOriginal
		}
		text = c.stripMention(text)
		text = bxConvertUserMentionsToReadable(text)
	}
	text = strings.TrimSpace(text)

	// Openline relays an external connector user with a sender tag at the start
	// of the text. We parse it to (a) echo the connector msgId back so the reply
	// routes to the right external message, and (b) — for the newer 3-token
	// "[Name] #uid #msgId" layout — derive a stable per-participant identity from
	// the external person's uid so each customer gets their own USER.md / memory
	// instead of collapsing into the shared connector proxy.
	//
	// Identity is gated on FromIsConnector: only genuine connector relays
	// (IS_CONNECTOR=Y) may mint a participant identity. An operator who types a
	// look-alike "[Name] #a #b" tag (observed live) must never be mistaken for a
	// customer, so for non-connector messages we only strip the tag from the body
	// the agent sees and derive nothing. Plain group chats / DMs → no-op.
	var senderTag OpenlineSenderTag
	var participantSenderID string
	if isOpenChannel {
		if evt.Params.FromIsConnector {
			senderTag = parseOpenlineSenderTag(text)
			if senderTag.Format == TagFormatThreeToken && senderTag.UID != "" {
				// Unique per (channel instance, OL chat, person) → one contact and
				// one USER.md per external customer in the chat. c.Name() is
				// config-controlled, the uid is digits-only from the regex, and the
				// DialogID is a validated "chatNN" token — no freeform injection.
				participantSenderID = fmt.Sprintf("openlines:%s:%s:%s",
					c.Name(), evt.Params.DialogID, senderTag.UID)
			}
			if senderTag.Format != TagFormatNone {
				text = strings.TrimSpace(senderTag.Rest)
			}
		} else if _, rest := extractOpenlineSenderPrefix(text, true); rest != text {
			// Operator/staff message: strip a look-alike tag from the body for the
			// LLM, but derive no identity and echo no prefix.
			text = strings.TrimSpace(rest)
		}
	}

	if text == "" && len(evt.Params.Files) == 0 {
		return
	}

	// senderID defaults to the connector proxy id (e.g. "960", shared by every
	// customer in the chat). When a per-participant identity was derived above we
	// use it instead so contact + memory scope to the individual person.
	senderID := evt.Params.FromUserID
	if participantSenderID != "" {
		senderID = participantSenderID
	}
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

	// Visibility: whisper (internal-only) vs public (forwarded to external
	// connector). Send() uses this to route through imbot.message.add with
	// SKIP_CONNECTOR=Y (whisper) or imbot.v2.Chat.Message.send (public).
	// Default to public so legacy events without the marker still publish
	// to the connector — that matches pre-refactor behavior.
	visibility := VisibilityPublic
	if evt.Params.IsHiddenMessage {
		visibility = VisibilityWhisper
	}
	meta := map[string]string{
		"bitrix_dialog_id": evt.Params.DialogID,
		"bitrix_portal":    c.portalDomainSafe(),
		"bitrix_bot_id":    strconv.Itoa(c.BotID()),
		"bitrix_bot_code":  c.cfg.BotCode,
		// Bitrix MESSAGE_ID drives the v2 fields.replyId reply-link. It is a
		// Bitrix-internal id, distinct from the connector msgId embedded in the
		// sender tag (echoed via MetaKeySenderPrefix instead), so it must stay the
		// genuine webhook MESSAGE_ID — a 13-digit connector msgId is not a valid
		// Bitrix replyId.
		MetaKeyMessageID:  evt.Params.MessageID,
		MetaKeyVisibility: visibility,
	}
	// Echo the connector sender tag back on the reply so the Open Channel
	// connector routes the answer to the right external message:
	//   - 3-token   → "#msgId" only (name + uid dropped; connector needs just the id),
	//   - legacy    → canonical "[name] #msgId" (unchanged from prior behavior),
	//   - name-only → "[name]".
	// senderTag is the zero value (TagFormatNone) for operator / non-connector
	// messages, so this is a no-op there.
	switch senderTag.Format {
	case TagFormatThreeToken:
		meta[MetaKeySenderPrefix] = "#" + senderTag.MsgID
	case TagFormatLegacy:
		meta[MetaKeySenderPrefix] = "[" + senderTag.Name + "] #" + senderTag.MsgID
	case TagFormatNameOnly:
		meta[MetaKeySenderPrefix] = "[" + senderTag.Name + "]"
	}
	// Per-participant identity signal for the consumer (per-person USER.md scope).
	if participantSenderID != "" {
		meta[MetaKeyParticipantUserID] = participantSenderID
	}
	if evt.Params.ReplyToMID != "" {
		meta["bitrix_reply_to_mid"] = evt.Params.ReplyToMID
	}
	if evt.Params.ChatID != "" {
		meta["bitrix_chat_id"] = evt.Params.ChatID
	}
	// Entity binding lets MCP tools resolve "this deal" / "this task" without
	// parsing CHAT_TITLE strings. Examples:
	//   bitrix_chat_entity_type=CRM        bitrix_chat_entity_id=DEAL|2064
	//   bitrix_chat_entity_type=TASKS_TASK bitrix_chat_entity_id=2704
	// Plain user-created chats omit both fields.
	if evt.Params.ChatEntityType != "" {
		meta["bitrix_chat_entity_type"] = evt.Params.ChatEntityType
	}
	if evt.Params.ChatEntityID != "" {
		meta["bitrix_chat_entity_id"] = evt.Params.ChatEntityID
	}

	// Collect contact for processed messages (matches Telegram pattern at
	// channels/telegram/handlers.go:617-630). Runs AFTER policy gating so
	// blocked senders aren't recorded, and BEFORE HandleMessage so the
	// contact row exists by the time the agent (on the other side of the
	// bus) resolves userID → MCP credentials via MCPServerStore.
	//
	// Bitrix24 webhooks don't ship display_name / username, so we enrich
	// via user.get on first sight (cached per-channel; see
	// contact_enrich.go). Best-effort — if the RPC fails or scope is
	// missing we still create the contact row with empty fields, which
	// matches the pre-enrichment behavior and causes no regression.
	if cc := c.ContactCollector(); cc != nil {
		var contactName, contactUsername string
		if participantSenderID != "" && senderTag.Name != "" {
			// Use the connector-parsed display name directly. resolveContactName
			// would call user.get(senderID), but senderID is now the synthetic
			// per-participant id and the numeric proxy (e.g. 960) resolves to the
			// connector account — not the customer. The parsed name is the only
			// real signal we have for the external person.
			contactName = senderTag.Name
		} else {
			contactName, contactUsername = c.resolveContactName(ctx, senderID)
		}
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, contactName, contactUsername, peerKind, "user", "", "")
		if isGroup && chatID != "" {
			cc.EnsureContact(ctx, c.Type(), c.Name(), chatID, "", "", "", "group", "group", "", "")
		}
	}

	// MCP lazy provisioning (Phase C). Best-effort: any failure is logged
	// and swallowed — agent loop downstream will just see no creds and skip
	// the MCP server's tools, which is strictly better UX than the channel
	// denying the message. The typed errors let tests assert behavior
	// without string matching.
	if err := c.provisionIfMissing(ctx, senderID, evt.Auth); err != nil {
		switch {
		case errors.Is(err, ErrProvisionDisabled),
			errors.Is(err, ErrProvisionSkippedOpenChannel),
			errors.Is(err, ErrProvisionDebounced):
			// Expected no-ops — don't spam logs. Debug level is enough
			// for troubleshooting "why didn't this user get MCP tools?"
			slog.Debug("bitrix24 mcp: provisioning skipped",
				"channel", c.Name(), "user", senderID, "reason", err)
		default:
			// Unexpected error (HTTP failure, persist failure, auth
			// validation). Warn so operators see it, but DO NOT return —
			// message still flows through to the agent.
			slog.Warn("bitrix24 mcp: provisioning failed",
				"channel", c.Name(), "user", senderID, "err", err)
			// Best-effort degradation notice so the user knows to contact
			// admin instead of silently getting tool-less replies. Debounced
			// 5min per-user inside the helper so a retry storm / sustained
			// outage won't spam the DM. See notifyUserOfMCPIssueOnce
			// docstring for the design rationale.
			c.notifyUserOfMCPIssueOnce(ctx, senderID, chatID)
		}
	}

	// Download any attachments via imbot.v2.File.download and forward them to
	// the agent with their MIME type preserved. Best-effort: failures are logged
	// inside downloadEventFiles and never block the text from reaching the agent.
	mediaFiles := c.downloadEventFiles(ctx, c.BotID(), evt.Params.Files)
	// Prepend <media:*> tags so the LLM sees the attachment alongside the body
	// text. The agent loop's enrichInputMedia REPLACES tags it finds (it does
	// NOT insert new ones), so without this step a Bitrix-borne PDF / audio
	// arrives as bare text — the LLM never realizes an attachment exists and
	// degrades to "no file attached" or hallucinates a CRM document. Match
	// the Telegram / Slack / Discord pattern (shared media.BuildMediaTags) so
	// the tag shape stays uniform across channels.
	if len(mediaFiles) > 0 {
		if tags := media.BuildMediaTags(mediaFilesToInfos(mediaFiles)); tags != "" {
			if text == "" {
				text = tags
			} else {
				text = tags + "\n" + text
			}
		}
	}
	slog.Info("bitrix24 message: publish to bus",
		"sender_id", senderID,
		"chat_id", chatID,
		"peer_kind", peerKind,
		"message_id", evt.Params.MessageID,
		"media_count", len(mediaFiles),
	)
	c.HandleMessageMedia(senderID, chatID, text, mediaFiles, meta, peerKind)
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
	if _, err := client.Call(ctx, "imbot.v2.Chat.Message.send", map[string]any{
		"botId":    botID,
		"dialogId": evt.Params.DialogID,
		"fields":   map[string]any{"message": welcome},
	}); err != nil {
		slog.Warn("bitrix24: welcome message send failed",
			"dialog_id", evt.Params.DialogID, "err", err)
	}
}

// isMentionedParams checks all three sources Bitrix24 may use to convey a
// bot mention, in authority order:
//
//  1. data[PARAMS][MENTIONED_LIST][<bot_id>] — structured map populated by
//     Bitrix on group messages. Highest authority (no regex, no Unicode
//     edge cases). Absent on DMs.
//  2. data[PARAMS][MESSAGE_ORIGINAL] — raw BBCode (`[USER=<bot_id>]…[/USER]`).
//     Group-only. Reliable when MENTIONED_LIST is absent (older portals).
//  3. data[PARAMS][MESSAGE] — stripped plain text. Bitrix removes the
//     @mention from this in group chats, so it only matches in DMs.
//
// Without this fallback chain group @mentions silently drop because
// MESSAGE has the mention stripped before the webhook is sent.
func (c *Channel) isMentionedParams(p *EventParams) bool {
	if p == nil {
		return false
	}
	botID := c.BotID()
	if botID <= 0 {
		return false
	}
	id := strconv.Itoa(botID)
	if _, ok := p.MentionedList[id]; ok {
		return true
	}
	if p.MessageOriginal != "" && c.isMentioned(p.MessageOriginal) {
		return true
	}
	return c.isMentioned(p.Message)
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
// Webhook events use short codes:
//
//   - "P" / "private" — direct message between two users
//   - "C" / "chat"    — generic multi-user group chat (also CRM Deal chats)
//   - "O" / "open"    — Open Channel session (customer-service widget)
//   - "X"             — entity-bound group chat (Tasks, Workgroups, etc.).
//     Observed empirically with CHAT_ENTITY_TYPE=TASKS_TASK; treated as
//     group because CHAT_USER_COUNT>1 and the @mention semantics match
//     plain "C" chats. Without this branch task chats fall through to
//     direct-message handling, which bypasses the require-mention gate
//     and routes traffic to a `direct:chatNN` session key instead of
//     `group:chatNN`, mixing per-task context into per-user history.
//   - "B"             — Bitrix24 workgroup / Collab (SONET_GROUP) chat,
//     observed with CHAT_TYPE=B and CHAT_ENTITY_TYPE=SONET_GROUP. Same
//     group semantics as "C" / "X" — multi-user by design, @mention
//     gating applies. Without this branch the @mention prefix never
//     renders on the bot's reply (it'd address the wrong member by
//     name) and per-user history bleeds into one direct session.
//
// Anything else (including the empty string) is treated as a direct
// message so stricter DM policies apply.
func isGroupMessageType(mt string) bool {
	switch strings.ToUpper(strings.TrimSpace(mt)) {
	case "B", "C", "CHAT", "O", "OPEN", "X":
		return true
	default:
		return false
	}
}
