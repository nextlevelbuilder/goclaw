package mattermost

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const placeholderText = "Thinking..."

// threadKey builds the composite local key for a thread. The ":thread:"
// suffix is what the gateway consumer uses for per-thread session isolation.
func threadKey(channelID, rootID string) string {
	return channelID + ":thread:" + rootID
}

// splitLocalKey splits a local key back into the channel ID and thread root
// ("" when the key has no thread suffix).
func splitLocalKey(key string) (channelID, rootID string) {
	if channelID, rootID, ok := strings.Cut(key, ":thread:"); ok {
		return channelID, rootID
	}
	return key, ""
}

func participKey(channelID, rootID string) string {
	return channelID + ":particip:" + rootID
}

// inboundPost is a post plus the channel context that came with its event
// (or was looked up, for edits).
type inboundPost struct {
	post        *mmPost
	channelType string // O public, P private, D direct, G group DM
	chatTitle   string
}

// flatChannel reports channel types where the bot answers every post in
// place, without mention gating or reply threads: DMs always, group DMs when
// group_dm_as_direct is on. Group DMs stay "group" peers either way, so
// participants never share a DM session or each other's DM-level access.
func (c *Channel) flatChannel(channelType string) bool {
	switch channelType {
	case "D":
		return true
	case "G":
		return c.cfg.GroupDMAsDirect != nil && *c.cfg.GroupDMAsDirect
	default:
		return false
	}
}

// ignoredPost reports posts the bot never handles: its own, system posts
// (joins, header changes, …) and posts from other bots.
func (c *Channel) ignoredPost(post *mmPost) bool {
	return post.UserID == "" || post.UserID == c.botUserID || post.Type != "" || post.fromBot()
}

func (c *Channel) handlePostedEvent(ctx context.Context, ev wsEvent) {
	post, err := parsePostedEvent(ev)
	if err != nil {
		slog.Debug("mattermost: skip malformed posted event", "error", err)
		return
	}
	if c.ignoredPost(post) {
		return
	}

	// Websocket reconnects may replay recent events.
	if _, loaded := c.dedup.LoadOrStore(post.ID, time.Now()); loaded {
		return
	}

	c.handlePost(ctx, inboundPost{
		post:        post,
		channelType: ev.dataString("channel_type"),
		chatTitle:   ev.dataString("channel_display_name"),
	})
}

// handlePostEditedEvent gives an edited channel post a second chance: when
// the edit adds an @mention to a post the bot has not answered yet, the post
// is handled as if it had just been sent. DM posts are always answered, so
// their edits are ignored.
func (c *Channel) handlePostEditedEvent(ctx context.Context, ev wsEvent) {
	post, err := parsePostedEvent(ev)
	if err != nil {
		slog.Debug("mattermost: skip malformed post_edited event", "error", err)
		return
	}
	// The bot's own streaming edits are most of these events: drop them
	// before any API call.
	if c.ignoredPost(post) {
		return
	}
	if _, answered := c.dispatched.Load(post.ID); answered {
		return
	}
	if !c.RequireMention() || !c.isBotMentioned(post.Message) {
		return
	}
	if time.Since(time.UnixMilli(post.CreateAt)) > editWindow {
		return
	}

	info, err := c.resolveChannel(ctx, post.ChannelID)
	if err != nil {
		slog.Debug("mattermost: cannot resolve channel of edited post", "channel_id", post.ChannelID, "error", err)
		return
	}
	if c.flatChannel(info.channelType) {
		return
	}

	c.handlePost(ctx, inboundPost{post: post, channelType: info.channelType, chatTitle: info.displayName})
}

func (c *Channel) handlePost(ctx context.Context, in inboundPost) {
	post := in.post
	ctx = store.WithTenantID(ctx, c.TenantID())
	channelID := post.ChannelID
	senderID := post.UserID

	isDM := in.channelType == "D"
	flat := c.flatChannel(in.channelType)
	gated := !flat && c.RequireMention()
	mentioned := c.isBotMentioned(post.Message)
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	displayName, username := c.resolveUser(ctx, senderID)

	if isDM {
		if !c.checkDMPolicy(ctx, senderID, channelID) {
			return
		}
	} else if !c.checkGroupPolicy(ctx, senderID, channelID, mentioned || !gated) {
		// Only a post addressed to the bot may get the pairing prompt, so an
		// unapproved channel is not flooded while people talk among themselves.
		return
	}

	content := post.Message

	var items []media.MediaInfo
	if fileItems, docContent := c.resolveMedia(ctx, post); len(fileItems) > 0 || docContent != "" {
		items = append(items, fileItems...)
		content = joinNonEmpty(content, docContent)
	}

	// Thread replies carry the root post as context, like a quoted reply.
	if post.RootID != "" {
		rootCtx, rootItems := c.fetchThreadRootContext(ctx, post.RootID)
		content = joinNonEmpty(rootCtx, content)
		items = append(rootItems, items...)
	}

	var mediaPaths []string
	var mediaFiles []bus.MediaFile
	if len(items) > 0 {
		content = joinNonEmpty(media.BuildMediaTags(items), content)
		for _, it := range items {
			if it.FilePath == "" {
				continue
			}
			mediaPaths = append(mediaPaths, it.FilePath)
			mediaFiles = append(mediaFiles, bus.MediaFile{Path: it.FilePath, MimeType: it.ContentType, Filename: it.FileName})
		}
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	// historyKey groups un-mentioned messages: the channel itself for
	// top-level posts, the thread for replies.
	historyKey := channelID
	parentHistoryKey := ""
	if post.RootID != "" {
		historyKey = threadKey(channelID, post.RootID)
		parentHistoryKey = channelID
	}

	// In channels the bot always answers in a thread, so the whole exchange
	// lives in one per-thread session. DMs (and flat group DMs) stay flat
	// unless the user threads.
	replyRootID := post.RootID
	if !flat && replyRootID == "" {
		replyRootID = post.ID
	}
	localKey := channelID
	if replyRootID != "" {
		localKey = threadKey(channelID, replyRootID)
	}

	if gated {
		if !mentioned && post.RootID != "" && c.threadTTL > 0 {
			key := participKey(channelID, post.RootID)
			if last, ok := c.threadParticip.Load(key); ok {
				if time.Since(last.(time.Time)) < c.threadTTL {
					mentioned = true
				} else {
					c.threadParticip.Delete(key)
				}
			}
		}

		if !mentioned {
			c.GroupHistory().Record(historyKey, channels.HistoryEntry{
				Sender:           displayName,
				SenderID:         senderID,
				Body:             content,
				ParentHistoryKey: parentHistoryKey,
				Media:            mediaPaths,
				Timestamp:        time.Now(),
				MessageID:        post.ID,
			}, c.HistoryLimit())

			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, displayName, username, "group", "user", "", "")
			}
			return
		}
	}

	content = strings.TrimSpace(c.stripBotMention(content))
	if content == "" {
		return
	}

	slog.Debug("mattermost message received",
		"sender_id", senderID, "channel_id", channelID,
		"is_dm", isDM, "preview", channels.Truncate(content, 50))

	// One placeholder per local key: a second post to the same DM or thread
	// (merged by the inbound debouncer, or a concurrent run) reuses the
	// pending one instead of leaving an orphaned "Thinking..." behind. The
	// first reply to arrive takes it; later ones are posted as new posts.
	if _, pending := c.placeholders.Load(localKey); !pending {
		if placeholder, err := c.api.createPost(ctx, &mmPost{
			ChannelID: channelID,
			RootID:    replyRootID,
			Message:   placeholderText,
		}); err == nil {
			c.placeholders.Store(localKey, placeholder.ID)
		} else {
			slog.Debug("mattermost: placeholder post failed", "channel_id", channelID, "error", err)
		}
	}
	// After the placeholder: clients clear the indicator when the typer posts.
	c.startTyping(localKey, channelID, replyRootID)

	finalContent := content
	if peerKind == "group" {
		annotated := fmt.Sprintf("[From: %s]\n%s", displayName, content)
		finalContent = annotated
		if c.HistoryLimit() > 0 {
			for _, p := range c.GroupHistory().CollectMedia(historyKey) {
				mediaFiles = append(mediaFiles, bus.MediaFile{Path: p, MimeType: media.DetectMIMEType(p)})
			}
			finalContent = c.GroupHistory().BuildContext(historyKey, annotated, c.HistoryLimit())
		}
	}

	metadata := map[string]string{
		"message_id":      post.ID,
		"user_id":         senderID,
		"username":        username,
		"display_name":    channels.SanitizeDisplayName(displayName),
		"channel_id":      channelID,
		"is_dm":           fmt.Sprintf("%t", isDM),
		"local_key":       localKey,
		"placeholder_key": localKey,
	}
	if replyRootID != "" {
		metadata["message_thread_id"] = replyRootID
	}
	if in.chatTitle != "" && !isDM {
		metadata[tools.MetaChatTitle] = in.chatTitle
	}

	c.HandleAuthorizedMessageMedia(senderID, channelID, finalContent, mediaFiles, metadata, peerKind)
	c.dispatched.Store(post.ID, time.Now())

	if peerKind == "group" {
		c.threadParticip.Store(participKey(channelID, replyRootID), time.Now())
		c.GroupHistory().Clear(historyKey)
	}
}

// fetchThreadRootContext returns the thread root post as reply context plus
// any media attached to it.
func (c *Channel) fetchThreadRootContext(ctx context.Context, rootID string) (string, []media.MediaInfo) {
	root, err := c.api.getPost(ctx, rootID)
	if err != nil {
		slog.Debug("mattermost: failed to fetch thread root", "root_id", rootID, "error", err)
		return "", nil
	}

	var replyCtx string
	if root.Message != "" {
		name := "bot"
		if root.UserID != c.botUserID {
			name, _ = c.resolveUser(ctx, root.UserID)
		}
		replyCtx = fmt.Sprintf("[Replying to %s]\n%s\n[/Replying]", name, channels.Truncate(root.Message, 500))
	}

	items, docContent := c.resolveMedia(ctx, root)
	for i := range items {
		items[i].FromReply = true
	}
	return joinNonEmpty(replyCtx, docContent), items
}

// isBotMentioned reports whether the text mentions the bot as @username.
// The server-computed "mentions" list of posted events is deliberately not
// used: it also contains everyone notified by @channel, @all, @here and
// group mentions, which are not requests to the bot.
func (c *Channel) isBotMentioned(text string) bool {
	return c.mentionRe != nil && c.mentionRe.MatchString(text)
}

// stripBotMention removes @botusername from the text.
func (c *Channel) stripBotMention(text string) string {
	if c.mentionRe == nil {
		return text
	}
	return c.mentionRe.ReplaceAllString(text, "${1}${2}")
}

// setBotIdentity records the bot account and compiles the @username matcher.
// Mattermost usernames may contain letters, digits, '.', '-' and '_'; a
// trailing '.' is treated as punctuation.
func (c *Channel) setBotIdentity(userID, username string) {
	c.botUserID = userID
	c.botUsername = username
	c.mentionRe = nil
	if username != "" {
		c.mentionRe = regexp.MustCompile(`(?i)(^|[^a-z0-9._-])@` + regexp.QuoteMeta(username) + `($|[^a-z0-9_-])`)
	}
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n\n" + b
	}
}

// --- Policy checks ---

func (c *Channel) checkDMPolicy(ctx context.Context, senderID, channelID string) bool {
	switch c.CheckDMPolicy(ctx, senderID, c.cfg.DMPolicy) {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, channelID)
		return false
	default:
		return false
	}
}

// checkGroupPolicy applies group_policy. notifyPairing controls whether an
// unpaired channel gets the pairing prompt.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, channelID string, notifyPairing bool) bool {
	groupPolicy := c.cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	// Allowlist accepts either the sender or the whole Mattermost channel.
	if groupPolicy == "allowlist" {
		if !c.HasAllowList() {
			return false
		}
		return c.IsAllowed(senderID) || c.IsAllowed(channelID)
	}

	switch c.CheckGroupPolicy(ctx, senderID, channelID, groupPolicy) {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		if notifyPairing {
			c.sendPairingReply(ctx, "group:"+channelID, channelID)
		}
		return false
	default:
		return false
	}
}

func (c *Channel) sendPairingReply(ctx context.Context, senderID, channelID string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}
	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), channelID, "default", nil)
	if err != nil {
		slog.Warn("mattermost: failed to request pairing code", "error", err)
		return
	}

	// Channels get the admin-approval template instead of the DM pairing prompt.
	var msg string
	if strings.HasPrefix(senderID, "group:") {
		msg = c.SystemMessage("", systemmessages.KeyPairingGroupPrivateRequired, systemmessages.Vars{
			"platform": "Mattermost",
			"code":     code,
		})
	} else {
		msg = c.SystemMessage("", systemmessages.KeyPairingAccountRequired, systemmessages.Vars{
			"platform":  "Mattermost",
			"sender_id": senderID,
			"code":      code,
		})
	}
	if _, err := c.api.createPost(ctx, &mmPost{ChannelID: channelID, Message: msg}); err != nil {
		slog.Warn("mattermost: failed to send pairing reply", "channel_id", channelID, "error", err)
		return
	}
	c.MarkPairingNotifSent(senderID)
}
