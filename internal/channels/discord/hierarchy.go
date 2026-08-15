package discord

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

const contactDisplayTitleMetadataKey = "display_title"

// ResolveGroupDisplayTitle resolves a display-only Discord title. A thread is
// qualified with its parent channel so identical thread names remain distinct.
func (c *Channel) ResolveGroupDisplayTitle(ctx context.Context, channelID string) (string, error) {
	ch := c.resolveDiscordChannel(ctx, channelID)
	if ch == nil {
		return "", nil
	}
	var parent *discordgo.Channel
	if ch.IsThread() && ch.ParentID != "" {
		parent = c.resolveDiscordChannel(ctx, ch.ParentID)
	}
	return discordDisplayTitle(ch, parent), nil
}

func (c *Channel) resolveCachedGroupDisplayTitle(channelID string) string {
	if c == nil || c.session == nil || c.session.State == nil || channelID == "" {
		return ""
	}
	ch, err := c.session.State.Channel(channelID)
	if err != nil || ch == nil {
		return ""
	}
	var parent *discordgo.Channel
	if ch.IsThread() && ch.ParentID != "" {
		parent, _ = c.session.State.Channel(ch.ParentID)
	}
	return discordDisplayTitle(ch, parent)
}

func discordDisplayTitle(ch, parent *discordgo.Channel) string {
	if ch == nil {
		return ""
	}
	name := channels.SanitizeDisplayName(ch.Name)
	if name == "" || !ch.IsThread() || parent == nil {
		return name
	}
	parentName := channels.SanitizeDisplayName(parent.Name)
	if parentName == "" || parentName == name {
		return name
	}
	return name + " / " + parentName
}

func discordContactMetadata(ch *discordgo.Channel, lookup func(string) *discordgo.Channel) map[string]string {
	if ch == nil || !ch.IsThread() || ch.ParentID == "" || lookup == nil {
		return nil
	}
	displayTitle := discordDisplayTitle(ch, lookup(ch.ParentID))
	if displayTitle == "" || displayTitle == strings.TrimSpace(ch.Name) {
		return nil
	}
	return map[string]string{contactDisplayTitleMetadataKey: displayTitle}
}
