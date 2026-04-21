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

// DiscordThreadCreatorFn creates a Discord thread on the named channel.
// Implemented by channels.Manager.CreateDiscordThread.
type DiscordThreadCreatorFn func(ctx context.Context, channel string, params channels.DiscordThreadParams) (channels.DiscordThreadResult, error)

// DiscordThreadCreatorAware tools can receive a DiscordThreadCreatorFn.
type DiscordThreadCreatorAware interface {
	SetDiscordThreadCreator(DiscordThreadCreatorFn)
}

// CreateDiscordThreadTool lets agents create a Discord thread (text or forum)
// on a Discord channel instance they have access to.
type CreateDiscordThreadTool struct {
	creator       DiscordThreadCreatorFn
	tenantChecker ChannelTenantChecker
}

// NewCreateDiscordThreadTool constructs the tool. Both the creator fn and the
// tenant checker are wired at gateway startup via the *Aware interfaces.
func NewCreateDiscordThreadTool() *CreateDiscordThreadTool {
	return &CreateDiscordThreadTool{}
}

// SetDiscordThreadCreator injects the channel-manager-backed creator fn.
func (t *CreateDiscordThreadTool) SetDiscordThreadCreator(fn DiscordThreadCreatorFn) {
	t.creator = fn
}

// SetChannelTenantChecker injects the tenant guard function.
func (t *CreateDiscordThreadTool) SetChannelTenantChecker(c ChannelTenantChecker) {
	t.tenantChecker = c
}

func (t *CreateDiscordThreadTool) Name() string { return "create_discord_thread" }

func (t *CreateDiscordThreadTool) RequiredChannelTypes() []string { return []string{"discord"} }

func (t *CreateDiscordThreadTool) Description() string {
	return "Create a new Discord thread in a text channel or post in a forum channel. " +
		"Returns the new thread's ID. To post messages into the thread afterwards, call the " +
		"`message` tool with chat_id set to the returned thread_id. Not available in DMs."
}

func (t *CreateDiscordThreadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Discord channel instance name (default: current channel from context).",
			},
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Parent Discord channel ID (text or forum). Default: current channel ID from context.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Thread name (1-100 characters).",
			},
			"message_id": map[string]any{
				"type":        "string",
				"description": "Optional. If set, the thread is rooted at this existing message.",
			},
			"auto_archive_minutes": map[string]any{
				"type":        "integer",
				"description": "Auto-archive duration in minutes. One of 60, 1440, 4320, 10080. Default 1440.",
			},
			"private": map[string]any{
				"type":        "boolean",
				"description": "If true, create a private thread (requires bot CREATE_PRIVATE_THREADS permission). Default false.",
			},
			"initial_message": map[string]any{
				"type":        "string",
				"description": "Required for forum channels (Discord API mandate). Ignored for text-channel threads — use the message tool with chat_id=thread_id to post afterwards.",
			},
			"applied_tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Forum-only. Tag IDs to apply to the new forum post.",
			},
		},
		"required": []string{"name"},
	}
}

func (t *CreateDiscordThreadTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.creator == nil {
		return ErrorResult("create_discord_thread: no Discord thread creator available")
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

	name, _ := args["name"].(string)
	if l := len([]rune(name)); l < 1 || l > 100 {
		return ErrorResult(fmt.Sprintf("name must be 1-100 characters (got %d)", l))
	}

	// Tenant gate: mirrors the check in MessageTool.validateChannelTenant.
	if t.tenantChecker != nil {
		chTenant, exists := t.tenantChecker(channel)
		if !exists {
			return ErrorResult(fmt.Sprintf("channel %q not found", channel))
		}
		if chTenant != uuid.Nil {
			ctxTenant := store.TenantIDFromContext(ctx)
			if ctxTenant != uuid.Nil && chTenant != ctxTenant {
				slog.Warn("security.cross_tenant_thread_blocked",
					"channel", channel, "channel_id", channelID,
					"ctx_tenant", ctxTenant, "ch_tenant", chTenant)
				return ErrorResult("channel not accessible from this tenant")
			}
		}
	}

	// DM rejection at the tool layer (peer kind from inbound context).
	// Belt-and-braces: CreateThread also rejects DM/GroupDM parent channels.
	if ToolPeerKindFromCtx(ctx) == "direct" {
		return ErrorResult("threads are not supported in Discord DMs")
	}

	private, _ := args["private"].(bool)
	params := channels.DiscordThreadParams{
		ChannelID:      channelID,
		Name:           name,
		MessageID:      argString(args, "message_id"),
		Private:        private,
		InitialMessage: argString(args, "initial_message"),
	}
	if v, ok := args["auto_archive_minutes"].(float64); ok {
		params.AutoArchiveMinutes = int(v)
	}
	if raw, ok := args["applied_tags"].([]any); ok {
		tags := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				tags = append(tags, s)
			}
		}
		params.AppliedTags = tags
	}

	result, err := t.creator(ctx, channel, params)
	if err != nil {
		return ErrorResult(fmt.Sprintf("creating discord thread: %v", err))
	}

	data, _ := json.Marshal(result)
	return NewResult(string(data))
}

