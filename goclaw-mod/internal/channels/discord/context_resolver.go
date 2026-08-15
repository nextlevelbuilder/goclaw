package discord

import (
	"context"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channelmemory"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (c *Channel) ResolveMemoryExtractionContext(ctx context.Context, inst *store.ChannelInstanceData, group store.PendingMessageGroup) (channelmemory.ExtractionContext, error) {
	out := channelmemory.ExtractionContext{
		Platform:        channels.TypeDiscord,
		ChannelInstance: group.ChannelName,
		HistoryKey:      group.HistoryKey,
		ChannelID:       group.HistoryKey,
		ParentChannelID: group.ParentHistoryKey,
	}
	if inst != nil && inst.Name != "" {
		out.ChannelInstance = inst.Name
	}
	ch := c.resolveDiscordChannel(ctx, group.HistoryKey)
	if ch == nil {
		return out, nil
	}
	out.ChannelID = ch.ID
	out.ChannelName = channels.SanitizeDisplayName(ch.Name)

	parentID := group.ParentHistoryKey
	if parentID == "" && ch.IsThread() {
		parentID = ch.ParentID
	}
	if parentID != "" {
		out.ParentChannelID = parentID
		parent := c.resolveDiscordChannel(ctx, parentID)
		if parent != nil {
			out.ParentChannelName = channels.SanitizeDisplayName(parent.Name)
			if parent.ParentID != "" {
				out.CategoryID = parent.ParentID
				if category := c.resolveDiscordChannel(ctx, parent.ParentID); category != nil {
					out.CategoryName = channels.SanitizeDisplayName(category.Name)
				}
			}
		}
	}
	if out.CategoryID == "" && ch.ParentID != "" && !ch.IsThread() {
		category := c.resolveDiscordChannel(ctx, ch.ParentID)
		if category != nil && category.Type == discordgo.ChannelTypeGuildCategory {
			out.CategoryID = category.ID
			out.CategoryName = channels.SanitizeDisplayName(category.Name)
		}
	}
	return out, nil
}

func (c *Channel) resolveDiscordChannel(ctx context.Context, channelID string) *discordgo.Channel {
	if c == nil || c.session == nil || channelID == "" {
		return nil
	}
	if c.session.State != nil {
		if ch, err := c.session.State.Channel(channelID); err == nil && ch != nil {
			return ch
		}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	ch, err := c.session.Channel(channelID, discordgo.WithContext(lookupCtx))
	if err != nil {
		slog.Debug("discord: extraction context channel lookup failed", "channel_id", channelID, "error", err)
		return nil
	}
	return ch
}
