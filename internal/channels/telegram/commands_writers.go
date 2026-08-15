package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// writersSelfHealTTL gates repeated enrichment attempts for the same
// (chatID,userID). Users who left the group will fail indefinitely; without
// the TTL every /writers call would hammer the Telegram API.
const writersSelfHealTTL = 5 * time.Minute

// writersSelfHealMaxPerCall caps the number of async enrichment goroutines
// spawned per /writers invocation so a group with many legacy rows can't
// burst-spam the bot API.
const writersSelfHealMaxPerCall = 5

// writerHealCacheMax caps the writerHealLastTry map so a long-lived bot
// accumulating entries across thousands of groups/users cannot grow the
// map unboundedly. When the cap is hit we prune any entry whose last
// attempt is older than 2×TTL — those would be eligible for retry anyway.
const writerHealCacheMax = 1000

// shouldTrySelfHeal returns true if no healing has been attempted for this
// (chatID,userID) within the TTL window. On return=true the timestamp is
// updated immediately so concurrent callers don't duplicate work.
func (c *Channel) shouldTrySelfHeal(key string) bool {
	c.writerHealMu.Lock()
	defer c.writerHealMu.Unlock()
	if c.writerHealLastTry == nil {
		c.writerHealLastTry = make(map[string]time.Time)
	}
	if last, ok := c.writerHealLastTry[key]; ok && time.Since(last) < writersSelfHealTTL {
		return false
	}
	// Opportunistic prune once the map gets large: drop entries whose last
	// attempt is older than 2× TTL. O(n) worst case but amortized cheap —
	// only runs when the cap is hit.
	if len(c.writerHealLastTry) >= writerHealCacheMax {
		cutoff := time.Now().Add(-2 * writersSelfHealTTL)
		for k, t := range c.writerHealLastTry {
			if t.Before(cutoff) {
				delete(c.writerHealLastTry, k)
			}
		}
	}
	c.writerHealLastTry[key] = time.Now()
	return true
}

// handleWriterCommand handles /addwriter and /removewriter commands.
// The target user is identified by replying to one of their messages.
func (c *Channel) handleWriterCommand(ctx context.Context, message *telego.Message, chatID int64, chatIDStr, senderID string, isGroup bool, setThread func(*telego.SendMessageParams), action string) {
	chatIDObj := tu.ID(chatID)

	send := func(text string) {
		msg := tu.Message(chatIDObj, text)
		setThread(msg)
		c.bot.SendMessage(ctx, msg)
	}

	if !isGroup {
		send("This command only works in group chats.")
		return
	}

	if c.configPermStore == nil {
		send("File writer management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("writer command: agent resolve failed", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)
	senderNumericID := strings.SplitN(senderID, "|", 2)[0]

	// Fetch existing writers (cached 60s) for both permission check and remove guard.
	// Bootstrap exception: if no writers exist yet, the first /addwriter caller
	// is allowed to bootstrap the allowlist.
	existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)

	if len(existingWriters) > 0 {
		isWriter := false
		for _, w := range existingWriters {
			if w.UserID == senderNumericID {
				isWriter = true
				break
			}
		}
		if !isWriter {
			send("Only existing file writers can manage the writer list.")
			return
		}
	} else if action == "remove" {
		send("No file writers configured yet. Use /addwriter to add the first one.")
		return
	}

	// Extract target user from reply-to message
	if message.ReplyToMessage == nil || message.ReplyToMessage.From == nil {
		verb := "add"
		if action == "remove" {
			verb = "remove"
		}
		send(fmt.Sprintf("To %s a writer: find a message from that person, swipe to reply it, then type /%swriter.", verb, verb))
		return
	}

	targetUser := message.ReplyToMessage.From
	targetID := fmt.Sprintf("%d", targetUser.ID)
	targetName := targetUser.FirstName
	if targetUser.Username != "" {
		targetName = "@" + targetUser.Username
	}

	switch action {
	case "add":
		meta, _ := json.Marshal(map[string]string{"displayName": targetUser.FirstName, "username": targetUser.Username})
		if err := c.configPermStore.Grant(ctx, &store.ConfigPermission{
			AgentID:    agentID,
			Scope:      groupID,
			ConfigType: store.ConfigTypeFileWriter,
			UserID:     targetID,
			Permission: "allow",
			Metadata:   meta,
		}); err != nil {
			slog.Warn("add writer failed", "error", err, "target", targetID)
			send("Failed to add writer. Please try again.")
			return
		}
		send(fmt.Sprintf("Added %s as a file writer.", targetName))

	case "remove":
		// Prevent removing the last writer (reuse cached existingWriters)
		if len(existingWriters) <= 1 {
			send("Cannot remove the last file writer.")
			return
		}
		if err := c.configPermStore.Revoke(ctx, agentID, groupID, store.ConfigTypeFileWriter, targetID); err != nil {
			slog.Warn("remove writer failed", "error", err, "target", targetID)
			send("Failed to remove writer. Please try again.")
			return
		}
		send(fmt.Sprintf("Removed %s from file writers.", targetName))
	}
}

// handleListWriters handles the /writers command.
func (c *Channel) handleListWriters(ctx context.Context, chatID int64, chatIDStr string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	chatIDObj := tu.ID(chatID)

	send := func(text string) {
		msg := tu.Message(chatIDObj, text)
		setThread(msg)
		c.bot.SendMessage(ctx, msg)
	}

	if !isGroup {
		send("This command only works in group chats.")
		return
	}

	if c.configPermStore == nil {
		send("File writer management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("list writers: agent resolve failed", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)

	writers, err := c.configPermStore.List(ctx, agentID, store.ConfigTypeFileWriter, groupID)
	if err != nil {
		slog.Warn("list writers failed", "error", err)
		send("Failed to list writers. Please try again.")
		return
	}

	if len(writers) == 0 {
		send("No file writers configured for this group. Use /addwriter to add one.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File writers for this group (%d):\n", len(writers)))
	// needsHeal collects rows whose metadata is empty so we can async-enrich
	// them after sending the response. Legacy rows (pre auto-enrichment) or
	// rows that failed enrichment at grant time get healed on list.
	var needsHeal []store.ConfigPermission
	for i, w := range writers {
		label := channels.WriterLabel(w.Metadata, w.UserID)
		// A label of the shape "User <id>" means neither username nor
		// displayName was present — flag the row for background enrichment.
		if strings.HasPrefix(label, "User ") {
			needsHeal = append(needsHeal, w)
		}
		sb.WriteString(fmt.Sprintf("%d. %s (ID: %s)\n", i+1, label, w.UserID))
	}
	send(sb.String())

	if len(needsHeal) > 0 {
		c.asyncHealWriterMetadata(agentID, chatIDStr, groupID, needsHeal)
	}
}

// asyncHealWriterMetadata fires a single background goroutine that walks
// up to writersSelfHealMaxPerCall rows and tries to enrich their metadata
// via Telegram's getChatMember, persisting results through the same Grant
// upsert path as /addwriter. Fire-and-forget; uses context.Background so
// it outlives the request that triggered it.
func (c *Channel) asyncHealWriterMetadata(agentID uuid.UUID, chatIDStr, groupID string, rows []store.ConfigPermission) {
	// Track the goroutine in handlerWg so Channel.Stop() waits for it before
	// tearing down the bot/store and avoids use-after-close panics.
	c.handlerWg.Add(1)
	go func(rows []store.ConfigPermission) {
		defer c.handlerWg.Done()
		for i, w := range rows {
			if i >= writersSelfHealMaxPerCall {
				return
			}
			key := chatIDStr + "|" + w.UserID
			if !c.shouldTrySelfHeal(key) {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			info, err := c.ResolveMember(ctx, chatIDStr, w.UserID)
			cancel()
			if err != nil {
				slog.Info("writers.self_heal.resolve_failed", "user_id", w.UserID, "error", err)
				continue
			}
			meta, err := channels.BuildWriterMetadata(info)
			if err != nil {
				continue
			}
			if err := c.configPermStore.Grant(context.Background(), &store.ConfigPermission{
				AgentID:    agentID,
				Scope:      groupID,
				ConfigType: store.ConfigTypeFileWriter,
				UserID:     w.UserID,
				Permission: "allow",
				Metadata:   meta,
			}); err != nil {
				slog.Warn("writers.self_heal.grant_failed", "user_id", w.UserID, "error", err)
				continue
			}
			slog.Info("writers.self_heal.ok", "user_id", w.UserID)
		}
	}(rows)
}
