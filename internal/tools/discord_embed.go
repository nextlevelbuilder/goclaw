package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DiscordEmbedSenderFn sends Discord rich embeds on the named channel.
// Implemented by channels.Manager.SendDiscordEmbed.
type DiscordEmbedSenderFn func(ctx context.Context, channel string, params channels.DiscordSendEmbedParams) (channels.DiscordSendEmbedResult, error)

// DiscordEmbedSenderAware tools can receive a DiscordEmbedSenderFn.
type DiscordEmbedSenderAware interface {
	SetDiscordEmbedSender(DiscordEmbedSenderFn)
}

// SendDiscordEmbedTool lets agents post Discord rich embeds on a Discord
// channel instance they have access to.
type SendDiscordEmbedTool struct {
	sender        DiscordEmbedSenderFn
	tenantChecker ChannelTenantChecker
}

// NewSendDiscordEmbedTool constructs the tool. Both the sender fn and the
// tenant checker are wired at gateway startup via the *Aware interfaces.
func NewSendDiscordEmbedTool() *SendDiscordEmbedTool {
	return &SendDiscordEmbedTool{}
}

// SetDiscordEmbedSender injects the channel-manager-backed sender fn.
func (t *SendDiscordEmbedTool) SetDiscordEmbedSender(fn DiscordEmbedSenderFn) {
	t.sender = fn
}

// SetChannelTenantChecker injects the tenant guard function.
func (t *SendDiscordEmbedTool) SetChannelTenantChecker(c ChannelTenantChecker) {
	t.tenantChecker = c
}

func (t *SendDiscordEmbedTool) Name() string { return "send_discord_embed" }

func (t *SendDiscordEmbedTool) RequiredChannelTypes() []string { return []string{"discord"} }

func (t *SendDiscordEmbedTool) Description() string {
	return "Post a Discord rich embed (or up to 10 embeds) on a Discord channel or thread. " +
		"Use embeds when you have STRUCTURED content that benefits from visual formatting: " +
		"status cards, search results, leaderboards, release notes, error reports, key-value " +
		"summaries, or anything with sections/fields. For a plain text reply, just return the " +
		"reply text normally — do NOT wrap ordinary answers in an embed. " +
		"\n\n" +
		"Quick recipes:" +
		"\n  * Success card: title + green color (0x2ECC71) + checkmark prefix in description." +
		"\n  * Error card: title + red color (0xE74C3C) + description with cause." +
		"\n  * Stats card: title + 3-6 fields with inline=true for a grid layout." +
		"\n  * Article card: title + url (makes title a link) + description + thumbnail." +
		"\n  * Alert: author (name + icon) + description + timestamp + footer for source." +
		"\n\n" +
		"Defaults: targets the current Discord channel from context — set `channel_id` only " +
		"to post elsewhere (e.g. a thread returned by create_discord_thread). " +
		"Limits: 10 embeds per call, 25 fields per embed, 6000 total chars across all embed " +
		"text, 2000 chars for `content`."
}

// embedFieldSchema is the JSON schema for a single embed field.
var embedFieldSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "Field label (max 256 chars, e.g. \"Status\", \"Owner\").",
		},
		"value": map[string]any{
			"type":        "string",
			"description": "Field value (max 1024 chars, supports markdown).",
		},
		"inline": map[string]any{
			"type":        "boolean",
			"description": "If true, Discord packs this field alongside adjacent inline fields (up to three per row). Use for compact stat grids.",
		},
	},
	"required": []string{"name", "value"},
}

// embedSchema is the JSON schema for a single embed object.
var embedSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"title": map[string]any{
			"type":        "string",
			"description": "Embed title shown in bold at the top (max 256 chars). Combine with `url` to make it a clickable link.",
		},
		"description": map[string]any{
			"type":        "string",
			"description": "Main body text (max 4096 chars, supports markdown including links and code blocks).",
		},
		"url": map[string]any{
			"type":        "string",
			"description": "Optional URL. Turns the title into a link.",
		},
		"color": map[string]any{
			"type":        "integer",
			"description": "Accent color as decimal RGB (e.g. 3447003 = blue, 5763719 = green, 15158332 = red, 15844367 = yellow). Hex-to-decimal: 0x5865F2 = 5793266 (Discord blurple).",
		},
		"timestamp": map[string]any{
			"type":        "string",
			"description": "ISO 8601 / RFC 3339 timestamp shown in the footer area (e.g. \"2026-04-21T15:04:05Z\").",
		},
		"footer": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":     map[string]any{"type": "string", "description": "Footer text (max 2048 chars)."},
				"icon_url": map[string]any{"type": "string", "description": "Small icon shown left of the footer text."},
			},
			"required":    []string{"text"},
			"description": "Footer block at the bottom of the embed.",
		},
		"image": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "Public http(s) URL of the image."},
			},
			"required":    []string{"url"},
			"description": "Large image shown below the description.",
		},
		"thumbnail": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "Public http(s) URL of the thumbnail."},
			},
			"required":    []string{"url"},
			"description": "Small image shown in the top-right corner of the embed.",
		},
		"author": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":     map[string]any{"type": "string", "description": "Author name shown at the top (max 256 chars)."},
				"url":      map[string]any{"type": "string", "description": "Makes the author name a link."},
				"icon_url": map[string]any{"type": "string", "description": "Avatar shown to the left of the author name."},
			},
			"required":    []string{"name"},
			"description": "Author block at the top of the embed (above the title).",
		},
		"fields": map[string]any{
			"type":        "array",
			"items":       embedFieldSchema,
			"description": "Up to 25 key/value rows. Set inline=true to pack adjacent fields on one line.",
		},
	},
}

func (t *SendDiscordEmbedTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Discord channel instance name (default: current channel from context).",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Target Discord channel or thread ID. Default: current channel ID from context. Use a thread_id returned by create_discord_thread to post into that thread.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Optional plain text shown above the embeds. 2000 char limit. Omit for embed-only messages.",
			},
			"reply_to": map[string]any{
				"type":        "string",
				"description": "Optional message ID to reply to. Creates a Discord inline reply pointing at that message.",
			},
			"embeds": map[string]any{
				"type":        "array",
				"items":       embedSchema,
				"description": "1 to 10 embeds to send in a single message. Combined text across all embeds is capped at 6000 chars by Discord.",
			},
		},
		"required": []string{"embeds"},
	}
}

func (t *SendDiscordEmbedTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.sender == nil {
		return ErrorResult("send_discord_embed: no Discord embed sender available")
	}

	channel, _ := args["channel"].(string)
	if channel == "" {
		channel = ToolChannelFromCtx(ctx)
	}
	if channel == "" {
		return ErrorResult("channel is required (no current channel in context)")
	}

	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		channelID = ToolChatIDFromCtx(ctx)
	}
	if channelID == "" {
		return ErrorResult("channel_id is required (no current channel_id in context)")
	}

	rawEmbeds, ok := args["embeds"].([]any)
	if !ok || len(rawEmbeds) == 0 {
		return ErrorResult("embeds is required (at least one embed)")
	}
	embeds := make([]channels.DiscordEmbed, 0, len(rawEmbeds))
	for i, raw := range rawEmbeds {
		m, ok := raw.(map[string]any)
		if !ok {
			return ErrorResult(fmt.Sprintf("embeds[%d] must be an object", i))
		}
		e, err := decodeEmbed(m)
		if err != nil {
			return ErrorResult(fmt.Sprintf("embeds[%d]: %v", i, err))
		}
		embeds = append(embeds, e)
	}

	// Tenant gate: mirrors the pattern from MessageTool / CreateDiscordThreadTool.
	if t.tenantChecker != nil {
		chTenant, exists := t.tenantChecker(channel)
		if !exists {
			return ErrorResult(fmt.Sprintf("channel %q not found", channel))
		}
		if chTenant != uuid.Nil {
			ctxTenant := store.TenantIDFromContext(ctx)
			if ctxTenant != uuid.Nil && chTenant != ctxTenant {
				slog.Warn("security.cross_tenant_embed_blocked",
					"channel", channel, "channel_id", channelID,
					"ctx_tenant", ctxTenant, "ch_tenant", chTenant)
				return ErrorResult("channel not accessible from this tenant")
			}
		}
	}

	params := channels.DiscordSendEmbedParams{
		ChannelID: channelID,
		Content:   argString(args, "content"),
		ReplyTo:   argString(args, "reply_to"),
		Embeds:    embeds,
	}

	result, err := t.sender(ctx, channel, params)
	if err != nil {
		return ErrorResult(fmt.Sprintf("sending discord embed: %v", err))
	}

	data, _ := json.Marshal(result)
	return NewResult(string(data))
}

// decodeEmbed converts an untyped tool-args embed map into the structured
// channels.DiscordEmbed. Field-level shape errors are returned here so the
// LLM gets an immediate, explicit error rather than a Discord API rejection.
func decodeEmbed(m map[string]any) (channels.DiscordEmbed, error) {
	out := channels.DiscordEmbed{
		Title:       argString(m, "title"),
		Description: argString(m, "description"),
		URL:         argString(m, "url"),
		Timestamp:   argString(m, "timestamp"),
	}
	if v, ok := m["color"].(float64); ok {
		out.Color = int(v)
	}

	if v, ok := m["footer"].(map[string]any); ok {
		out.Footer = &channels.DiscordEmbedFooter{
			Text:    argString(v, "text"),
			IconURL: argString(v, "icon_url"),
		}
	}
	if v, ok := m["image"].(map[string]any); ok {
		out.Image = &channels.DiscordEmbedMedia{URL: argString(v, "url")}
	}
	if v, ok := m["thumbnail"].(map[string]any); ok {
		out.Thumbnail = &channels.DiscordEmbedMedia{URL: argString(v, "url")}
	}
	if v, ok := m["author"].(map[string]any); ok {
		out.Author = &channels.DiscordEmbedAuthor{
			Name:    argString(v, "name"),
			URL:     argString(v, "url"),
			IconURL: argString(v, "icon_url"),
		}
	}
	if raw, ok := m["fields"].([]any); ok {
		out.Fields = make([]channels.DiscordEmbedField, 0, len(raw))
		for i, r := range raw {
			fm, ok := r.(map[string]any)
			if !ok {
				return channels.DiscordEmbed{}, fmt.Errorf("fields[%d] must be an object", i)
			}
			inline, _ := fm["inline"].(bool)
			out.Fields = append(out.Fields, channels.DiscordEmbedField{
				Name:   argString(fm, "name"),
				Value:  argString(fm, "value"),
				Inline: inline,
			})
		}
	}

	return out, nil
}
