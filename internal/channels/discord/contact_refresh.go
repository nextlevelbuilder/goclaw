package discord

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const contactRefreshInterval = time.Hour

const contactRefreshPageSize = 100

type contactRefreshResult struct {
	report channels.MetadataRefreshReport
}

// RefreshContactCache forces a Discord metadata refresh into channel_contacts.
func (c *Channel) RefreshContactCache(ctx context.Context) (channels.MetadataRefreshReport, error) {
	if c == nil || c.session == nil {
		return channels.MetadataRefreshReport{}, fmt.Errorf("discord session unavailable")
	}
	if c.ContactCollector() == nil {
		return channels.MetadataRefreshReport{}, fmt.Errorf("contact collector unavailable")
	}
	result := c.refreshContactCache(ctx)
	return result.report, nil
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

func (c *Channel) refreshContactCache(ctx context.Context) contactRefreshResult {
	result := contactRefreshResult{}
	if c == nil || c.session == nil {
		return result
	}
	cc := c.ContactCollector()
	if cc == nil {
		return result
	}
	cacheCtx := ctx
	if tenantID := c.TenantID(); tenantID != uuid.Nil {
		cacheCtx = store.WithTenantID(ctx, tenantID)
	}
	contactGroupIDs, err := c.storedGroupIDs(cacheCtx, cc)
	if err != nil {
		result.report.Errors = append(result.report.Errors, "list contact targets: "+err.Error())
	}
	result.report.ContactTargets = len(contactGroupIDs)
	pendingGroupIDs, err := c.pendingHistoryGroupIDs(cacheCtx)
	if err != nil {
		result.report.Errors = append(result.report.Errors, "list pending-message targets: "+err.Error())
	}
	result.report.PendingMessageTargets = len(pendingGroupIDs)
	targets := mergeRefreshTargets(contactGroupIDs, pendingGroupIDs)

	count := 0
	seen := make(map[string]struct{})
	knownChannels := make(map[string]*discordgo.Channel)
	for _, ch := range c.stateChannels() {
		if ch != nil && ch.ID != "" {
			knownChannels[ch.ID] = ch
		}
	}
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
		cc.RefreshContactWithMetadata(cacheCtx, c.Type(), c.Name(), ch.ID, "", title, "", "group", "group", "", "", discordContactMetadata(ch, func(id string) *discordgo.Channel {
			return knownChannels[id]
		}))
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

	for _, member := range c.stateMembers() {
		upsertMember(member)
	}
	for _, guildID := range c.guildIDs() {
		select {
		case <-ctx.Done():
			return result
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
			if ch != nil && ch.ID != "" {
				knownChannels[ch.ID] = ch
			}
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
				if ch != nil && ch.ID != "" {
					knownChannels[ch.ID] = ch
				}
			}
			for _, ch := range activeThreads.Threads {
				upsert(ch)
			}
		}
	}
	for _, ch := range knownChannels {
		upsert(ch)
	}
	for channelID, source := range targets {
		if _, ok := seen[channelID]; ok {
			result.report.LiveTargets++
			continue
		}
		result.report.DirectLookupAttempts++
		lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ch, err := c.session.Channel(channelID, discordgo.WithContext(lookupCtx))
		cancel()
		if err != nil {
			slog.Debug("discord stored group title lookup failed", "channel", c.Name(), "channel_id", channelID, "error", err)
			result.report.Failures = append(result.report.Failures, channels.MetadataRefreshFailure{ChannelID: channelID, Source: source, Reason: err.Error()})
			slog.Warn("discord metadata refresh group lookup failed", "channel", c.Name(), "channel_id", channelID, "source", source, "error", err)
			continue
		}
		if ch == nil || ch.ID == "" {
			result.report.Failures = append(result.report.Failures, channels.MetadataRefreshFailure{ChannelID: channelID, Source: source, Reason: "Discord returned no channel metadata"})
			continue
		}
		if channels.SanitizeDisplayName(ch.Name) == "" {
			result.report.Failures = append(result.report.Failures, channels.MetadataRefreshFailure{ChannelID: channelID, Source: source, Reason: "Discord returned an empty channel name"})
			continue
		}
		knownChannels[ch.ID] = ch
		parentResolved := true
		if ch.IsThread() && ch.ParentID != "" && knownChannels[ch.ParentID] == nil {
			parentCtx, parentCancel := context.WithTimeout(ctx, 10*time.Second)
			parent, parentErr := c.session.Channel(ch.ParentID, discordgo.WithContext(parentCtx))
			parentCancel()
			if parentErr != nil {
				slog.Debug("discord stored thread parent lookup failed", "channel", c.Name(), "channel_id", ch.ID, "parent_id", ch.ParentID, "error", parentErr)
				parentResolved = false
				result.report.Failures = append(result.report.Failures, channels.MetadataRefreshFailure{ChannelID: channelID, Source: source, Reason: "parent lookup: " + parentErr.Error()})
				slog.Warn("discord metadata refresh thread parent lookup failed", "channel", c.Name(), "channel_id", ch.ID, "parent_id", ch.ParentID, "source", source, "error", parentErr)
			} else if parent != nil && parent.ID != "" {
				knownChannels[parent.ID] = parent
				upsert(parent)
			} else {
				parentResolved = false
				result.report.Failures = append(result.report.Failures, channels.MetadataRefreshFailure{ChannelID: channelID, Source: source, Reason: "Discord returned no parent metadata"})
			}
		}
		upsert(ch)
		if parentResolved {
			result.report.DirectLookupResolved++
		}
	}
	result.report.GroupsRefreshed = count
	result.report.UsersRefreshed = userCount
	result.report.OK = len(result.report.Errors) == 0 && len(result.report.Failures) == 0
	slog.Info("discord contact cache refreshed", "channel", c.Name(), "groups", count, "users", userCount, "contact_targets", result.report.ContactTargets, "pending_message_targets", result.report.PendingMessageTargets, "live_targets", result.report.LiveTargets, "direct_lookup_attempts", result.report.DirectLookupAttempts, "direct_lookup_resolved", result.report.DirectLookupResolved, "failures", len(result.report.Failures), "errors", len(result.report.Errors))
	return result
}

func (c *Channel) pendingHistoryGroupIDs(ctx context.Context) ([]string, error) {
	if c == nil || c.GroupHistory() == nil {
		return nil, nil
	}
	return c.GroupHistory().PersistedGroupIDs(ctx)
}

func mergeRefreshTargets(contactIDs, pendingIDs []string) map[string]string {
	sources := make(map[string]map[string]struct{}, len(contactIDs)+len(pendingIDs))
	add := func(ids []string, source string) {
		for _, id := range ids {
			if id == "" {
				continue
			}
			if sources[id] == nil {
				sources[id] = make(map[string]struct{})
			}
			sources[id][source] = struct{}{}
		}
	}
	add(contactIDs, "contacts")
	add(pendingIDs, "pending_messages")
	targets := make(map[string]string, len(sources))
	for id, set := range sources {
		parts := make([]string, 0, len(set))
		for source := range set {
			parts = append(parts, source)
		}
		sort.Strings(parts)
		targets[id] = strings.Join(parts, ",")
	}
	return targets
}

func (c *Channel) storedGroupIDs(ctx context.Context, cc *store.ContactCollector) ([]string, error) {
	if c == nil || cc == nil {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var ids []string
	snapshotAt := time.Now().UTC()
	for offset := 0; ; offset += contactRefreshPageSize {
		contacts, err := cc.ListContacts(ctx, store.ContactListOpts{
			ChannelType:      c.Type(),
			ChannelInstance:  c.Name(),
			ContactType:      "group",
			SnapshotAt:       &snapshotAt,
			OrderByFirstSeen: true,
			Limit:            contactRefreshPageSize,
			Offset:           offset,
		})
		if err != nil {
			slog.Warn("discord stored group list failed", "channel", c.Name(), "error", err)
			return ids, err
		}
		for _, contact := range contacts {
			if contact.SenderID == "" {
				continue
			}
			if _, ok := seen[contact.SenderID]; ok {
				continue
			}
			seen[contact.SenderID] = struct{}{}
			ids = append(ids, contact.SenderID)
		}
		if len(contacts) < contactRefreshPageSize {
			break
		}
	}
	return ids, nil
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
