package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (c *Channel) handleEventsAPI(evt socketmode.Event) {
	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	// Ack immediately (Slack requires ack within ~3s)
	c.sm.Ack(*evt.Request)

	switch ev := eventsAPI.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		c.handleMessage(ev)
	case *slackevents.AppMentionEvent:
		c.handleAppMention(ev)
	}
}

func (c *Channel) handleMessage(ev *slackevents.MessageEvent) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())
	// For message_changed: extract user/text from the nested Message field.
	// Only process if the edit introduces a new @bot mention.
	if ev.SubType == "message_changed" {
		if ev.Message == nil {
			return
		}
		// Skip bot's own edits or messages without a user
		if ev.Message.User == c.botUserID || ev.Message.User == "" {
			return
		}
		// Only process if the edited message mentions the bot
		if !c.isBotMentioned(ev.Message.Text) {
			return
		}
		// Check that the previous version did NOT mention the bot (newly added mention)
		if ev.PreviousMessage != nil && c.isBotMentioned(ev.PreviousMessage.Text) {
			return
		}
		// Promote nested fields to top-level for unified processing below
		ev.User = ev.Message.User
		ev.Text = ev.Message.Text
		ev.TimeStamp = ev.Message.Timestamp
		ev.ThreadTimeStamp = ev.Message.ThreadTimestamp
	}

	if ev.User == c.botUserID || ev.User == "" {
		return
	}

	// Skip message subtypes (edits, deletes, bot_message, joins, etc.)
	// Allow "file_share" and "message_changed" subtypes.
	if ev.SubType != "" && ev.SubType != "file_share" && ev.SubType != "message_changed" {
		return
	}

	// Explicit dedup: prevent duplicate processing on Socket Mode reconnect
	dedupKey := ev.Channel + ":" + ev.TimeStamp
	if _, loaded := c.dedup.LoadOrStore(dedupKey, time.Now()); loaded {
		return
	}

	senderID := ev.User
	channelID := ev.Channel
	content := ev.Text

	isDM := ev.ChannelType == "im"
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	displayName := c.resolveDisplayName(senderID)

	// Policy check
	if isDM {
		if !c.checkDMPolicy(ctx, senderID, channelID) {
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, senderID, channelID) {
			return
		}
	}

	// For DMs, apply global allowlist filter (allow_from contains user IDs).
	// For groups, skip — group policy already handles channel/user filtering.
	if isDM && !c.IsAllowed(senderID) {
		slog.Debug("slack message rejected by allowlist",
			"user_id", senderID, "display_name", displayName)
		return
	}

	// Process file attachments from Slack message
	var mediaPaths []string
	var allItems []mediaItem
	if ev.Message != nil && len(ev.Message.Files) > 0 {
		items, docContent := c.resolveMedia(ev.Message.Files)
		allItems = append(allItems, items...)

		if docContent != "" {
			if content != "" {
				content = content + "\n\n" + docContent
			} else {
				content = docContent
			}
		}
	}

	// Fetch reply context + media from thread parent message.
	// Only when replying in a thread (ThreadTimeStamp != TimeStamp).
	threadTS := ev.ThreadTimeStamp
	if threadTS != "" && threadTS != ev.TimeStamp {
		replyCtx, replyItems := c.fetchThreadParentContext(context.Background(), channelID, threadTS)
		if replyCtx != "" {
			if content != "" {
				content = replyCtx + "\n\n" + content
			} else {
				content = replyCtx
			}
		}
		// Reply media first (context), current media second.
		if len(replyItems) > 0 {
			allItems = append(replyItems, allItems...)
		}
	}

	// Build media tags and collect file paths from all items.
	if len(allItems) > 0 {
		mediaTags := buildMediaTags(allItems)
		if mediaTags != "" {
			if content != "" {
				content = mediaTags + "\n\n" + content
			} else {
				content = mediaTags
			}
		}
		for _, item := range allItems {
			if item.FilePath != "" {
				mediaPaths = append(mediaPaths, item.FilePath)
			}
		}
	}

	if content == "" {
		return
	}

	// Determine local_key and thread context
	localKey := channelID
	if threadTS != "" {
		localKey = fmt.Sprintf("%s:thread:%s", channelID, threadTS)
	}

	// Mention gating in groups (with thread participation cache)
	if !isDM && c.requireMention {
		mentioned := c.isBotMentioned(content)

		// Thread participation cache: auto-reply in threads where bot previously participated
		if !mentioned && threadTS != "" && c.threadTTL > 0 {
			participKey := channelID + ":particip:" + threadTS
			if lastReply, ok := c.threadParticip.Load(participKey); ok {
				if time.Since(lastReply.(time.Time)) < c.threadTTL {
					mentioned = true
					slog.Debug("slack: auto-reply in participated thread",
						"channel_id", channelID, "thread_ts", threadTS)
				} else {
					c.threadParticip.Delete(participKey)
				}
			}
		}

		if !mentioned {
			c.groupHistory.Record(localKey, channels.HistoryEntry{
				Sender:    displayName,
				SenderID:  senderID,
				Body:      content,
				Media:     mediaPaths,
				Timestamp: time.Now(),
				MessageID: ev.TimeStamp,
			}, c.historyLimit)

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, displayName, "", "group", "user", "", "")
			}

			slog.Debug("slack group message recorded (no mention)",
				"channel_id", channelID, "user", displayName)
			return
		}
	}

	content = c.stripBotMention(content)
	content = strings.TrimSpace(content)

	slog.Debug("slack message received",
		"sender_id", senderID, "channel_id", channelID,
		"is_dm", isDM, "preview", channels.Truncate(content, 50))

	// Send "Thinking..." placeholder
	replyThreadTS := threadTS
	if !isDM && replyThreadTS == "" {
		replyThreadTS = ev.TimeStamp // start thread from the triggering message
	}

	placeholderOpts := []slackapi.MsgOption{
		slackapi.MsgOptionText("Thinking...", false),
	}
	if replyThreadTS != "" {
		placeholderOpts = append(placeholderOpts, slackapi.MsgOptionTS(replyThreadTS))
	}

	_, placeholderTS, err := c.api.PostMessage(channelID, placeholderOpts...)
	if err == nil {
		c.placeholders.Store(localKey, placeholderTS)
	}

	// Build final content with group history context
	finalContent := content
	if peerKind == "group" {
		annotated := fmt.Sprintf("[From: %s]\n%s", displayName, content)
		if c.historyLimit > 0 {
			// Collect media from pending history (files downloaded by earlier non-mentioned messages).
			if histMediaPaths := c.groupHistory.CollectMedia(localKey); len(histMediaPaths) > 0 {
				mediaPaths = append(mediaPaths, histMediaPaths...)
			}
			finalContent = c.groupHistory.BuildContext(localKey, annotated, c.historyLimit)
		} else {
			finalContent = annotated
		}
	}

	metadata := map[string]string{
		"message_id":      ev.TimeStamp,
		"user_id":         senderID,
		"username":        displayName,
		"display_name":    displayName,
		"channel_id":      channelID,
		"is_dm":           fmt.Sprintf("%t", isDM),
		"local_key":       localKey,
		"placeholder_key": localKey,
	}
	if replyThreadTS != "" {
		metadata["message_thread_id"] = replyThreadTS
	}

	// Message debounce: batch rapid messages per-thread
	if c.debounceDelay > 0 {
		if c.debounceMessage(localKey, senderID, channelID, finalContent, mediaPaths, metadata, peerKind) {
			// Record thread participation even when debounced
			if peerKind == "group" && replyThreadTS != "" {
				participKey := channelID + ":particip:" + replyThreadTS
				c.threadParticip.Store(participKey, time.Now())
			}
			return
		}
	}

	c.HandleMessage(senderID, channelID, finalContent, mediaPaths, metadata, peerKind)

	// Record thread participation for auto-reply cache
	if peerKind == "group" {
		if replyThreadTS != "" {
			participKey := channelID + ":particip:" + replyThreadTS
			c.threadParticip.Store(participKey, time.Now())
		}
		c.groupHistory.Clear(localKey)
	}
}

// fetchThreadParentContext fetches the thread-start message and returns a formatted
// reply context string + any downloaded media from the parent message.
func (c *Channel) fetchThreadParentContext(ctx context.Context, channelID, threadTS string) (string, []mediaItem) {
	params := &slackapi.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    threadTS,
		Limit:     1,
		Inclusive:  true,
	}
	history, err := c.api.GetConversationHistoryContext(ctx, params)
	if err != nil || len(history.Messages) == 0 {
		slog.Debug("slack: failed to fetch thread parent", "channel", channelID, "thread_ts", threadTS, "error", err)
		return "", nil
	}

	parent := &history.Messages[0]

	// Build reply context text.
	var replyCtx string
	if parent.Text != "" {
		body := channels.Truncate(parent.Text, 500)
		userName := c.resolveDisplayName(parent.User)
		replyCtx = fmt.Sprintf("[Replying to %s]\n%s\n[/Replying]", userName, body)
	}

	// Download files from parent message.
	var replyItems []mediaItem
	if len(parent.Files) > 0 {
		items, docContent := c.resolveMedia(parent.Files)
		for i := range items {
			items[i].FromReply = true
		}
		replyItems = items
		// Append extracted document text to reply context.
		if docContent != "" {
			if replyCtx != "" {
				replyCtx += "\n\n" + docContent
			} else {
				replyCtx = docContent
			}
		}
	}

	return replyCtx, replyItems
}
