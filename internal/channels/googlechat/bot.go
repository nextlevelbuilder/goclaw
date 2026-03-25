package googlechat

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// handleMessageEvent processes an inbound MESSAGE event.
func (c *Channel) handleMessageEvent(ctx context.Context, event *SpaceEvent) {
	if event == nil || event.Message == nil {
		return
	}

	messageName := event.Message.Name
	if messageName == "" {
		return
	}

	// 1. Dedup
	if c.isDuplicate(messageName) {
		slog.Debug("googlechat message deduplicated", "message", messageName)
		return
	}

	// 2. Extract sender info (skip bot messages to prevent self-echo)
	senderID, senderName := extractSenderInfo(event)
	if senderID == "" {
		return
	}
	if event.Message.Sender != nil && event.Message.Sender.Type == "BOT" {
		return
	}

	// 3. Determine peer kind
	spaceName := ""
	peerKind := "direct"
	if event.Space != nil {
		spaceName = event.Space.Name
		if event.Space.Type != "DM" {
			peerKind = "group"
		}
	}
	if spaceName == "" {
		return
	}

	// 4. Policy check
	dmPolicy := c.cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}
	groupPolicy := c.cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "pairing"
	}

	if !c.CheckPolicy(peerKind, dmPolicy, groupPolicy, senderID) {
		slog.Debug("googlechat message rejected by policy",
			"sender_id", senderID, "space", spaceName, "peer_kind", peerKind)
		return
	}

	// 5. RequireMention gating for groups
	mentioned := isBotMentioned(event)
	if peerKind == "group" {
		requireMention := true
		if c.cfg.RequireMention != nil {
			requireMention = *c.cfg.RequireMention
		}
		if requireMention && !mentioned {
			c.groupHistory.Record(spaceName, channels.HistoryEntry{
				Sender:    senderName,
				Body:      event.Message.Text,
				Timestamp: time.Now(),
				MessageID: messageName,
			}, c.historyLimit)
			return
		}
	}

	// 6. Build content — use argumentText (bot mention stripped) when mentioned, else full text
	content := event.Message.ArgumentText
	if content == "" {
		content = event.Message.Text
	}
	if content == "" {
		content = "[empty message]"
	}

	// 7. Group context
	if peerKind == "group" && senderName != "" {
		annotated := fmt.Sprintf("[From: %s]\n%s", senderName, content)
		if c.historyLimit > 0 {
			content = c.groupHistory.BuildContext(spaceName, annotated, c.historyLimit)
		} else {
			content = annotated
		}
	}

	// 8. Build metadata
	metadata := map[string]string{
		"message_name":  messageName,
		"platform":      "googlechat",
		"sender_name":   senderName,
		"mentioned_bot": fmt.Sprintf("%t", mentioned),
	}

	slog.Debug("googlechat message received",
		"sender_id", senderID, "space", spaceName,
		"peer_kind", peerKind, "preview", channels.Truncate(content, 50))

	// 9. Publish to bus
	c.HandleMessage(senderID, spaceName, content, nil, metadata, peerKind)

	// 10. Clear pending history after sending
	if peerKind == "group" {
		c.groupHistory.Clear(spaceName)
	}
}

// isDuplicate returns true if messageName was already processed.
func (c *Channel) isDuplicate(messageName string) bool {
	_, loaded := c.dedup.LoadOrStore(messageName, struct{}{})
	if !loaded {
		time.AfterFunc(5*time.Minute, func() {
			c.dedup.Delete(messageName)
		})
	}
	return loaded
}
