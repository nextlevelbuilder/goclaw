package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handlePosted processes new post events from the Mattermost WebSocket.
func (c *Channel) handlePosted(evt *MMWebSocketEvent) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())

	postJSON, ok := evt.Data["post"].(string)
	if !ok {
		return
	}

	var post MMPost
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		slog.Debug("mattermost: failed to parse post", "error", err)
		return
	}

	// Skip bot's own messages
	if post.UserId == c.botUserID {
		return
	}

	// Skip system messages
	if post.Type != "" {
		return
	}

	// Dedup: prevent duplicate processing on WebSocket reconnect
	dedupKey := post.ChannelId + ":" + post.Id
	if _, loaded := c.dedup.LoadOrStore(dedupKey, time.Now()); loaded {
		return
	}

	c.processPost(ctx, &post)
}

// handlePostEdited processes edited post events.
func (c *Channel) handlePostEdited(evt *MMWebSocketEvent) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())

	postJSON, ok := evt.Data["post"].(string)
	if !ok {
		return
	}

	var post MMPost
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		return
	}

	// Skip bot's own edits
	if post.UserId == c.botUserID {
		return
	}

	// Only process if the edit introduces a new @bot mention
	if !c.isBotMentioned(&post) {
		return
	}

	// Check it was already processed
	dedupKey := post.ChannelId + ":" + post.Id + ":edited"
	if _, loaded := c.dedup.LoadOrStore(dedupKey, time.Now()); loaded {
		return
	}

	c.processPost(ctx, &post)
}

// processPost is the unified handler for both new and edited posts.
func (c *Channel) processPost(ctx context.Context, post *MMPost) {
	senderID := post.UserId
	channelID := post.ChannelId
	content := post.Message

	// Determine DM vs Group
	channelType, ok := c.getChannelType(ctx, channelID)
	if !ok {
		return
	}
	isDM := channelType == ChannelTypeDirect || channelType == ChannelTypeGroup
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	// Compound sender ID format: "userID|displayName"
	// Used by the gateway's contact collector and session routing to identify
	// both the platform user ID and a human-readable name in a single string.
	// The pipe "|" is stripped from displayName to prevent parsing corruption.
	displayName := strings.ReplaceAll(c.resolveDisplayName(senderID), "|", "_")
	compoundSenderID := fmt.Sprintf("%s|%s", senderID, displayName)

	// Policy check
	if !c.CheckPolicy(peerKind, c.config.DMPolicy, c.config.GroupPolicy, compoundSenderID) {
		return
	}

	// For DMs, apply global allowlist filter
	if isDM && !c.IsAllowed(compoundSenderID) {
		slog.Debug("mattermost message rejected by allowlist",
			"user_id", senderID, "display_name", displayName)
		return
	}

	// Process file attachments
	// resolveMedia returns a cleanup func that removes all temp files.
	// MUST be deferred to avoid filling up the VPS disk with orphaned downloads.
	var mediaPaths []string
	var mediaCleanup func()
	if len(post.FileIds) > 0 {
		mediaPaths, mediaCleanup = c.resolveMedia(ctx, post.FileIds)
		if mediaCleanup != nil {
			defer mediaCleanup()
		}
	}

	if content == "" && len(mediaPaths) == 0 {
		return
	}

	// Determine local_key and thread context
	localKey := channelID
	rootID := post.RootId
	if !isDM && rootID != "" {
		localKey = fmt.Sprintf("%s:thread:%s", channelID, rootID)
	}

	// Mention gating in groups (with thread participation cache)
	if !isDM && c.requireMention {
		mentioned := c.isBotMentioned(post)

		// Thread participation cache: auto-reply in threads where bot previously participated
		if !mentioned && rootID != "" && c.threadTTL > 0 {
			participKey := channelID + ":particip:" + rootID
			if lastReply, ok := c.threadParticip.Load(participKey); ok {
				if time.Since(lastReply.(time.Time)) < c.threadTTL {
					mentioned = true
					slog.Debug("mattermost: auto-reply in participated thread",
						"channel_id", channelID, "root_id", rootID)
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
				Timestamp: time.Now(),
				MessageID: post.Id,
			}, c.historyLimit)

			// Collect contact even when bot is not mentioned
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, displayName, "", "group")
			}

			slog.Debug("mattermost group message recorded (no mention)",
				"channel_id", channelID, "user", displayName)
			return
		}
	}

	content = c.stripBotMention(content)
	content = strings.TrimSpace(content)

	slog.Debug("mattermost message received",
		"sender_id", senderID, "channel_id", channelID,
		"is_dm", isDM, "preview", channels.Truncate(content, 50))

	// Determine reply root for threading
	replyRootID := rootID
	if !isDM && replyRootID == "" {
		replyRootID = post.Id // start thread from the triggering message
	}

	// Build final content with group history context
	finalContent := content
	if peerKind == "group" {
		annotated := fmt.Sprintf("[From: %s]\n%s", displayName, content)
		if c.historyLimit > 0 {
			finalContent = c.groupHistory.BuildContext(localKey, annotated, c.historyLimit)
		} else {
			finalContent = annotated
		}
	}

	metadata := map[string]string{
		"message_id":      post.Id,
		"user_id":         senderID,
		"username":        displayName,
		"channel_id":      channelID,
		"is_dm":           fmt.Sprintf("%t", isDM),
		"local_key":       localKey,
		"placeholder_key": localKey,
	}
	if replyRootID != "" {
		metadata["message_thread_id"] = replyRootID
	}

	// Message debounce: batch rapid messages per-thread
	if c.debounceDelay > 0 {
		if c.debounceMessage(localKey, compoundSenderID, channelID, finalContent, mediaPaths, metadata, peerKind) {
			// Record thread participation even when debounced
			if peerKind == "group" && replyRootID != "" {
				participKey := channelID + ":particip:" + replyRootID
				c.threadParticip.Store(participKey, time.Now())
			}
			return
		}
	}

	c.HandleMessage(compoundSenderID, channelID, finalContent, mediaPaths, metadata, peerKind)

	// Record thread participation for auto-reply cache
	if peerKind == "group" {
		if replyRootID != "" {
			participKey := channelID + ":particip:" + replyRootID
			c.threadParticip.Store(participKey, time.Now())
		}
		c.groupHistory.Clear(localKey)
	}
}

// getChannelType retrieves the channel type (DM, group, public, private) from Mattermost API.
func (c *Channel) getChannelType(ctx context.Context, channelID string) (string, bool) {
	ch, err := c.client.GetChannel(ctx, channelID)
	if err != nil {
		slog.Warn("mattermost: failed to get channel info", "channel_id", channelID, "error", err)
		return "", false
	}
	return ch.Type, true
}

// debounceMessage batches rapid messages. Returns true if the message was debounced.
func (c *Channel) debounceMessage(localKey, senderID, chatID, content string, media []string, metadata map[string]string, peerKind string) bool {
	c.debounceMu.Lock()
	entry, exists := c.debounceTimers[localKey]
	if !exists {
		entry = &debounceEntry{
			senderID: senderID,
			chatID:   chatID,
			peerKind: peerKind,
		}
		c.debounceTimers[localKey] = entry
	}
	c.debounceMu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.timer != nil {
		entry.timer.Stop()
	}

	// Accumulate content
	if entry.content != "" {
		entry.content += "\n" + content
	} else {
		entry.content = content
	}
	entry.media = append(entry.media, media...)
	entry.metadata = metadata
	entry.senderID = senderID

	entry.timer = time.AfterFunc(c.debounceDelay, func() {
		c.flushDebounce(localKey)
	})

	return exists // first message is not debounced
}

// flushDebounce sends the accumulated debounced message.
func (c *Channel) flushDebounce(localKey string) {
	c.debounceMu.Lock()
	entry, ok := c.debounceTimers[localKey]
	if ok {
		delete(c.debounceTimers, localKey)
	}
	c.debounceMu.Unlock()

	if !ok {
		return
	}

	entry.mu.Lock()
	content := entry.content
	media := entry.media
	metadata := entry.metadata
	senderID := entry.senderID
	chatID := entry.chatID
	peerKind := entry.peerKind
	entry.mu.Unlock()

	if content == "" && len(media) == 0 {
		return
	}

	c.HandleMessage(senderID, chatID, content, media, metadata, peerKind)
}
