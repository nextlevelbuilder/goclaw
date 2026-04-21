package discord

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Discord embed limits. These mirror the Discord API limits documented at
// https://discord.com/developers/docs/resources/channel#embed-object-embed-limits.
// We enforce them in the channel layer so the agent gets a clear error instead
// of a less-friendly Discord API rejection.
const (
	embedTitleMax       = 256
	embedDescriptionMax = 4096
	embedFooterTextMax  = 2048
	embedAuthorNameMax  = 256
	embedFieldNameMax   = 256
	embedFieldValueMax  = 1024
	embedMaxFields      = 25
	embedTotalCharMax   = 6000 // combined across all embeds in a single message
	embedMaxPerMessage  = 10
	messageContentMax   = 2000
)

// embedAPI abstracts the discordgo calls used by SendEmbed so tests can stub
// the Discord API surface without needing a live session. The live
// implementation (sessionEmbedAPI) wraps c.session directly.
type embedAPI interface {
	channelMessageSendComplex(ctx context.Context, channelID string, data *discordgo.MessageSend) (*discordgo.Message, error)
}

type sessionEmbedAPI struct{ s *discordgo.Session }

func (a sessionEmbedAPI) channelMessageSendComplex(ctx context.Context, channelID string, data *discordgo.MessageSend) (*discordgo.Message, error) {
	return a.s.ChannelMessageSendComplex(channelID, data, discordgo.WithContext(ctx))
}

// SendEmbed sends one or more Discord rich embeds (optionally with accompanying
// plain-text content) to the target channel. Implements channels.DiscordEmbedSender.
func (c *Channel) SendEmbed(ctx context.Context, params channels.DiscordSendEmbedParams) (channels.DiscordSendEmbedResult, error) {
	return sendEmbed(ctx, sessionEmbedAPI{s: c.session}, params)
}

func sendEmbed(ctx context.Context, api embedAPI, params channels.DiscordSendEmbedParams) (channels.DiscordSendEmbedResult, error) {
	if params.ChannelID == "" {
		return channels.DiscordSendEmbedResult{}, errors.New("channel_id is required")
	}
	if len(params.Embeds) == 0 {
		return channels.DiscordSendEmbedResult{}, errors.New("at least one embed is required")
	}
	if len(params.Embeds) > embedMaxPerMessage {
		return channels.DiscordSendEmbedResult{}, fmt.Errorf("too many embeds: %d (max %d per message)", len(params.Embeds), embedMaxPerMessage)
	}
	if l := len([]rune(params.Content)); l > messageContentMax {
		return channels.DiscordSendEmbedResult{}, fmt.Errorf("content exceeds %d characters (got %d)", messageContentMax, l)
	}

	embeds := make([]*discordgo.MessageEmbed, 0, len(params.Embeds))
	// Discord's 6000-char cap applies to embed text only. message.content has
	// its own 2000 cap (enforced above) and does NOT count toward this total.
	// Earlier this function counted params.Content here, rejecting legitimate
	// Content+Embeds sends with a spurious "combined embed text exceeds 6000".
	total := 0
	for i := range params.Embeds {
		e, chars, err := convertEmbed(params.Embeds[i])
		if err != nil {
			return channels.DiscordSendEmbedResult{}, fmt.Errorf("embed[%d]: %w", i, err)
		}
		total += chars
		if total > embedTotalCharMax {
			return channels.DiscordSendEmbedResult{}, fmt.Errorf("combined embed text exceeds %d characters (at embed[%d])", embedTotalCharMax, i)
		}
		embeds = append(embeds, e)
	}

	send := &discordgo.MessageSend{
		Content: params.Content,
		Embeds:  embeds,
	}
	if params.ReplyTo != "" {
		send.Reference = &discordgo.MessageReference{
			MessageID: params.ReplyTo,
			ChannelID: params.ChannelID,
		}
	}

	msg, err := api.channelMessageSendComplex(ctx, params.ChannelID, send)
	if err != nil {
		return channels.DiscordSendEmbedResult{}, fmt.Errorf("discord API: %w", err)
	}
	if msg == nil {
		return channels.DiscordSendEmbedResult{}, errors.New("discord API returned nil message")
	}

	return channels.DiscordSendEmbedResult{
		MessageID: msg.ID,
		ChannelID: msg.ChannelID,
	}, nil
}

// convertEmbed validates a channels.DiscordEmbed and converts it to the
// discordgo shape. Returns the converted embed plus the rune count of all
// text fields, used by the caller to enforce the 6000-char per-message cap.
func convertEmbed(in channels.DiscordEmbed) (*discordgo.MessageEmbed, int, error) {
	if l := len([]rune(in.Title)); l > embedTitleMax {
		return nil, 0, fmt.Errorf("title exceeds %d characters (got %d)", embedTitleMax, l)
	}
	if l := len([]rune(in.Description)); l > embedDescriptionMax {
		return nil, 0, fmt.Errorf("description exceeds %d characters (got %d)", embedDescriptionMax, l)
	}
	if len(in.Fields) > embedMaxFields {
		return nil, 0, fmt.Errorf("too many fields: %d (max %d)", len(in.Fields), embedMaxFields)
	}

	chars := len([]rune(in.Title)) + len([]rune(in.Description))

	out := &discordgo.MessageEmbed{
		Title:       in.Title,
		Description: in.Description,
		URL:         in.URL,
		Color:       in.Color,
	}

	if in.Timestamp != "" {
		if _, err := time.Parse(time.RFC3339, in.Timestamp); err != nil {
			return nil, 0, fmt.Errorf("timestamp must be ISO 8601 / RFC 3339: %w", err)
		}
		out.Timestamp = in.Timestamp
	}

	if in.Footer != nil {
		if in.Footer.Text == "" {
			return nil, 0, errors.New("footer.text is required when footer is set")
		}
		if l := len([]rune(in.Footer.Text)); l > embedFooterTextMax {
			return nil, 0, fmt.Errorf("footer.text exceeds %d characters (got %d)", embedFooterTextMax, l)
		}
		chars += len([]rune(in.Footer.Text))
		out.Footer = &discordgo.MessageEmbedFooter{
			Text:    in.Footer.Text,
			IconURL: in.Footer.IconURL,
		}
	}

	if in.Image != nil {
		if in.Image.URL == "" {
			return nil, 0, errors.New("image.url is required when image is set")
		}
		out.Image = &discordgo.MessageEmbedImage{URL: in.Image.URL}
	}

	if in.Thumbnail != nil {
		if in.Thumbnail.URL == "" {
			return nil, 0, errors.New("thumbnail.url is required when thumbnail is set")
		}
		out.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: in.Thumbnail.URL}
	}

	if in.Author != nil {
		if in.Author.Name == "" {
			return nil, 0, errors.New("author.name is required when author is set")
		}
		if l := len([]rune(in.Author.Name)); l > embedAuthorNameMax {
			return nil, 0, fmt.Errorf("author.name exceeds %d characters (got %d)", embedAuthorNameMax, l)
		}
		chars += len([]rune(in.Author.Name))
		out.Author = &discordgo.MessageEmbedAuthor{
			Name:    in.Author.Name,
			URL:     in.Author.URL,
			IconURL: in.Author.IconURL,
		}
	}

	if len(in.Fields) > 0 {
		out.Fields = make([]*discordgo.MessageEmbedField, 0, len(in.Fields))
		for i, f := range in.Fields {
			if f.Name == "" {
				return nil, 0, fmt.Errorf("fields[%d].name is required", i)
			}
			if f.Value == "" {
				return nil, 0, fmt.Errorf("fields[%d].value is required", i)
			}
			if l := len([]rune(f.Name)); l > embedFieldNameMax {
				return nil, 0, fmt.Errorf("fields[%d].name exceeds %d characters (got %d)", i, embedFieldNameMax, l)
			}
			if l := len([]rune(f.Value)); l > embedFieldValueMax {
				return nil, 0, fmt.Errorf("fields[%d].value exceeds %d characters (got %d)", i, embedFieldValueMax, l)
			}
			chars += len([]rune(f.Name)) + len([]rune(f.Value))
			out.Fields = append(out.Fields, &discordgo.MessageEmbedField{
				Name:   f.Name,
				Value:  f.Value,
				Inline: f.Inline,
			})
		}
	}

	return out, chars, nil
}
