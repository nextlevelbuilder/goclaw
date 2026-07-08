package discord

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const contactRefreshInterval = time.Hour

// RefreshContactCache forces a Discord metadata refresh into channel_contacts.
func (c *Channel) RefreshContactCache(ctx context.Context) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("discord session unavailable")
	}
	if c.ContactCollector() == nil {
		return fmt.Errorf("contact collector unavailable")
	}
	c.refreshContactCache(ctx)
	return nil
}

func (c *Channel) runContactRefreshLoop(ctx context.Context) {
	if c == nil || c.session == nil || c.ContactCollector() == nil {
		return
	}
	c.refreshContactCache(ctx)

	ticker := time.NewTicker(contactRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshContactCache(ctx)
		}
	}
}

func (c *Channel) refreshContactCache(ctx context.Context) {
	if c == nil || c.session == nil {
		return
	}
	cc := c.ContactCollector()
	if cc == nil {
		return
	}
	cacheCtx := ctx
	if tenantID := c.TenantID(); tenantID != uuid.Nil {
		cacheCtx = store.WithTenantID(ctx, tenantID)
	}

	count := 0
	seen := make(map[string]struct{})
	userCount := 0
	seenUsers := make(map[string]struct{})
	upsert := func(ch *discordgo.Channel) {
		if ch == nil || ch.ID == "" {
			return
		}
		if _, ok := seen[ch.ID]; ok {
			return
		}
		title := channels.SanitizeDisplayName(ch.Name)
		if title == "" {
			return
		}
		seen[ch.ID] = struct{}{}
		cc.RefreshContact(cacheCtx, c.Type(), c.Name(), ch.ID, "", title, "", "group", "group", "", "")
		count++
	}
	upsertMember := func(member *discordgo.Member) {
		if member == nil || member.User == nil || member.User.ID == "" {
			return
		}
		if _, ok := seenUsers[member.User.ID]; ok {
			return
		}
		displayName := discordDisplayName(member.User, member)
		handle := discordHandle(member.User)
		if displayName == "" && handle == "" {
			return
		}
		seenUsers[member.User.ID] = struct{}{}
		cc.RefreshContact(cacheCtx, c.Type(), c.Name(), member.User.ID, member.User.ID, displayName, handle, "group", "user", "", "")
		userCount++
	}

	for _, ch := range c.stateChannels() {
		upsert(ch)
	}
	for _, member := range c.stateMembers() {
		upsertMember(member)
	}
	for _, guildID := range c.guildIDs() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		guildChannels, err := c.session.GuildChannels(guildID, discordgo.WithContext(lookupCtx))
		cancel()
		if err != nil {
			slog.Debug("discord contact cache channel sync failed", "channel", c.Name(), "guild_id", guildID, "error", err)
			continue
		}
		for _, ch := range guildChannels {
			upsert(ch)
		}

		threadCtx, threadCancel := context.WithTimeout(ctx, 10*time.Second)
		activeThreads, err := c.session.GuildThreadsActive(guildID, discordgo.WithContext(threadCtx))
		threadCancel()
		if err != nil {
			slog.Debug("discord contact cache thread sync failed", "channel", c.Name(), "guild_id", guildID, "error", err)
			continue
		}
		if activeThreads != nil {
			for _, ch := range activeThreads.Threads {
				upsert(ch)
			}
		}
	}
	if count > 0 || userCount > 0 {
		slog.Debug("discord contact cache refreshed", "channel", c.Name(), "groups", count, "users", userCount)
	}
}

func (c *Channel) guildIDs() []string {
	if c == nil || c.session == nil || c.session.State == nil {
		return nil
	}
	c.session.State.RLock()
	defer c.session.State.RUnlock()

	ids := make([]string, 0, len(c.session.State.Guilds))
	seen := make(map[string]struct{}, len(c.session.State.Guilds))
	for _, guild := range c.session.State.Guilds {
		if guild == nil || guild.ID == "" {
			continue
		}
		if _, ok := seen[guild.ID]; ok {
			continue
		}
		seen[guild.ID] = struct{}{}
		ids = append(ids, guild.ID)
	}
	return ids
}

func (c *Channel) stateChannels() []*discordgo.Channel {
	if c == nil || c.session == nil || c.session.State == nil {
		return nil
	}
	c.session.State.RLock()
	defer c.session.State.RUnlock()

	var out []*discordgo.Channel
	for _, guild := range c.session.State.Guilds {
		if guild == nil {
			continue
		}
		out = append(out, guild.Channels...)
		out = append(out, guild.Threads...)
	}
	return out
}

func (c *Channel) stateMembers() []*discordgo.Member {
	if c == nil || c.session == nil || c.session.State == nil {
		return nil
	}
	c.session.State.RLock()
	defer c.session.State.RUnlock()

	var out []*discordgo.Member
	for _, guild := range c.session.State.Guilds {
		if guild == nil {
			continue
		}
		out = append(out, guild.Members...)
	}
	return out
}
