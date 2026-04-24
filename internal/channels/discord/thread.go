package discord

import (
	"context"
	"errors"
	"fmt"

	"github.com/cartridge-gg/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// threadAPI abstracts the discordgo calls used by CreateThread so tests can stub them.
// The live implementation (sessionThreadAPI) wraps c.session directly.
type threadAPI interface {
	stateChannel(channelID string) (*discordgo.Channel, error)
	restChannel(ctx context.Context, channelID string) (*discordgo.Channel, error)
	messageThreadStartComplex(ctx context.Context, channelID, messageID string, data *discordgo.ThreadStart) (*discordgo.Channel, error)
	threadStartComplex(ctx context.Context, channelID string, data *discordgo.ThreadStart) (*discordgo.Channel, error)
	forumThreadStartComplex(ctx context.Context, channelID string, threadData *discordgo.ThreadStart, messageData *discordgo.MessageSend) (*discordgo.Channel, error)
}

type sessionThreadAPI struct{ s *discordgo.Session }

func (a sessionThreadAPI) stateChannel(channelID string) (*discordgo.Channel, error) {
	if a.s.State == nil {
		return nil, discordgo.ErrStateNotFound
	}
	return a.s.State.Channel(channelID)
}

func (a sessionThreadAPI) restChannel(ctx context.Context, channelID string) (*discordgo.Channel, error) {
	return a.s.Channel(channelID, discordgo.WithContext(ctx))
}

func (a sessionThreadAPI) messageThreadStartComplex(ctx context.Context, channelID, messageID string, data *discordgo.ThreadStart) (*discordgo.Channel, error) {
	return a.s.MessageThreadStartComplex(channelID, messageID, data, discordgo.WithContext(ctx))
}

func (a sessionThreadAPI) threadStartComplex(ctx context.Context, channelID string, data *discordgo.ThreadStart) (*discordgo.Channel, error) {
	return a.s.ThreadStartComplex(channelID, data, discordgo.WithContext(ctx))
}

func (a sessionThreadAPI) forumThreadStartComplex(ctx context.Context, channelID string, threadData *discordgo.ThreadStart, messageData *discordgo.MessageSend) (*discordgo.Channel, error) {
	return a.s.ForumThreadStartComplex(channelID, threadData, messageData, discordgo.WithContext(ctx))
}

// CreateThread creates a Discord thread. Implements channels.DiscordThreadCreator.
//
// Dispatch:
//   - Parent is DM / GroupDM → rejected (Discord does not support threads in DMs).
//   - Parent is forum channel → ForumThreadStartComplex (requires params.InitialMessage).
//   - params.MessageID != "" → MessageThreadStartComplex (thread rooted at that message).
//   - Otherwise → ThreadStartComplex (standalone thread). params.InitialMessage is ignored.
func (c *Channel) CreateThread(ctx context.Context, params channels.DiscordThreadParams) (channels.DiscordThreadResult, error) {
	return createThread(ctx, sessionThreadAPI{s: c.session}, params)
}

func createThread(ctx context.Context, api threadAPI, params channels.DiscordThreadParams) (channels.DiscordThreadResult, error) {
	if params.ChannelID == "" {
		return channels.DiscordThreadResult{}, errors.New("channel_id is required")
	}
	if params.Name == "" {
		return channels.DiscordThreadResult{}, errors.New("name is required")
	}
	if l := len([]rune(params.Name)); l < 1 || l > 100 {
		return channels.DiscordThreadResult{}, fmt.Errorf("name must be 1-100 characters (got %d)", l)
	}

	parent, err := lookupChannel(ctx, api, params.ChannelID)
	if err != nil {
		return channels.DiscordThreadResult{}, fmt.Errorf("lookup parent channel: %w", err)
	}

	switch parent.Type {
	case discordgo.ChannelTypeDM, discordgo.ChannelTypeGroupDM:
		return channels.DiscordThreadResult{}, errors.New("threads are not supported in DMs")
	}

	archive := params.AutoArchiveMinutes
	if archive == 0 {
		archive = 1440
	}
	threadType := discordgo.ChannelTypeGuildPublicThread
	if params.Private {
		threadType = discordgo.ChannelTypeGuildPrivateThread
	}

	ts := &discordgo.ThreadStart{
		Name:                params.Name,
		AutoArchiveDuration: archive,
		Type:                threadType,
		Invitable:           false,
	}

	var created *discordgo.Channel

	switch {
	case parent.Type == discordgo.ChannelTypeGuildForum:
		if params.InitialMessage == "" {
			return channels.DiscordThreadResult{}, errors.New("initial_message is required for forum channels")
		}
		ts.AppliedTags = params.AppliedTags
		msg := &discordgo.MessageSend{Content: params.InitialMessage}
		created, err = api.forumThreadStartComplex(ctx, params.ChannelID, ts, msg)
	case params.MessageID != "":
		created, err = api.messageThreadStartComplex(ctx, params.ChannelID, params.MessageID, ts)
	default:
		created, err = api.threadStartComplex(ctx, params.ChannelID, ts)
	}

	if err != nil {
		return channels.DiscordThreadResult{}, fmt.Errorf("discord API: %w", err)
	}
	if created == nil {
		return channels.DiscordThreadResult{}, errors.New("discord API returned nil channel")
	}

	return channels.DiscordThreadResult{
		ThreadID:        created.ID,
		Name:            created.Name,
		ParentChannelID: params.ChannelID,
		IsForum:         parent.Type == discordgo.ChannelTypeGuildForum,
	}, nil
}

// lookupChannel prefers the in-memory state cache and falls back to REST on miss.
func lookupChannel(ctx context.Context, api threadAPI, channelID string) (*discordgo.Channel, error) {
	if ch, err := api.stateChannel(channelID); err == nil && ch != nil {
		return ch, nil
	}
	return api.restChannel(ctx, channelID)
}
