package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// TelegramManagerFunc executes a whitelisted Telegram management action on a
// named channel instance. Implemented by channels.Manager.ManageTelegram.
type TelegramManagerFunc func(ctx context.Context, channel string, req channels.TelegramManagerRequest) (channels.TelegramManagerResult, error)

// TelegramManagerAware tools can receive a Telegram manager function.
type TelegramManagerAware interface {
	SetTelegramManager(TelegramManagerFunc)
}

// TelegramManagerTool exposes selected Telegram Bot API management actions to agents.
type TelegramManagerTool struct {
	manager TelegramManagerFunc
}

func NewTelegramManagerTool() *TelegramManagerTool { return &TelegramManagerTool{} }

func (t *TelegramManagerTool) SetTelegramManager(m TelegramManagerFunc) { t.manager = m }

func (t *TelegramManagerTool) RequiredChannelTypes() []string { return []string{"telegram"} }

func (t *TelegramManagerTool) Name() string { return "telegram_manager" }

func (t *TelegramManagerTool) Description() string {
	return "Manage Telegram chats through whitelisted Bot API actions. Supports chat info, member admin actions, message pin/delete actions, and forum topic management when the bot has the required Telegram permissions."
}

func (t *TelegramManagerTool) Parameters() map[string]any {
	actions := make([]string, 0, len(telegramManagerActions))
	for action := range telegramManagerActions {
		actions = append(actions, action)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Whitelisted Telegram management action, e.g. chat.get, member.ban, message.pin, topic.create.",
				"enum":        actions,
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "GoClaw Telegram channel instance name. Defaults to current channel.",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Telegram chat ID or @username. Defaults to current chat.",
			},
			"message_thread_id": map[string]any{"type": "integer", "description": "Telegram forum topic thread ID."},
			"message_id":        map[string]any{"type": "integer", "description": "Telegram message ID."},
			"user_id":           map[string]any{"type": "integer", "description": "Telegram user ID."},
			"name":              map[string]any{"type": "string", "description": "Topic name, general topic name, or other Telegram name parameter."},
			"text":              map[string]any{"type": "string", "description": "Text parameter for actions that need text."},
			"invite_link":       map[string]any{"type": "string", "description": "Telegram invite link for revoke actions."},
			"icon_color":        map[string]any{"type": "integer", "description": "Telegram forum topic icon color."},
			"icon_custom_emoji_id": map[string]any{
				"type":        "string",
				"description": "Telegram custom emoji ID for forum topic icon.",
			},
			"expire_date":          map[string]any{"type": "integer", "description": "Unix timestamp when an invite link expires."},
			"member_limit":         map[string]any{"type": "integer", "description": "Maximum members for an invite link."},
			"disable_notification": map[string]any{"type": "boolean", "description": "Disable notification for pin actions."},
			"creates_join_request": map[string]any{"type": "boolean", "description": "Whether invite link joins require admin approval."},
			"only_if_banned":       map[string]any{"type": "boolean", "description": "Only unban if currently banned."},
			"revoke_messages":      map[string]any{"type": "boolean", "description": "Revoke prior messages when banning."},
			"params":               map[string]any{"type": "object", "description": "Reserved structured options for future whitelisted actions."},
		},
		"required": []string{"action"},
	}
}

func (t *TelegramManagerTool) Execute(ctx context.Context, args map[string]any) *Result {
	if ToolChannelTypeFromCtx(ctx) != "telegram" {
		return ErrorResult("telegram_manager is only available inside Telegram channel runs")
	}
	permissions := TelegramManagerPermissionsFromCtx(ctx)
	if len(permissions) == 0 {
		return ErrorResult("telegram_manager is disabled for this Telegram channel")
	}
	if t.manager == nil {
		return ErrorResult("telegram_manager: no Telegram manager available")
	}

	action := argString(args, "action")
	if action == "" {
		return ErrorResult("action is required")
	}
	if _, ok := telegramManagerActions[action]; !ok {
		return ErrorResult(fmt.Sprintf("unsupported telegram_manager action: %s", action))
	}
	permissionGroup := telegramManagerPermissionGroup(action)
	if permissionGroup == "" || !telegramManagerPermissionAllowed(permissions, permissionGroup) {
		return ErrorResult(fmt.Sprintf("telegram_manager action %s is not allowed by this channel", action))
	}

	channel := argString(args, "channel")
	if channel == "" {
		channel = ToolChannelFromCtx(ctx)
	}
	if channel == "" {
		return ErrorResult("channel is required (no current channel in context)")
	}

	chatID := argString(args, "chat_id")
	if chatID == "" {
		chatID = ToolChatIDFromCtx(ctx)
	}

	req := channels.TelegramManagerRequest{
		Action:              action,
		ChatID:              chatID,
		MessageThreadID:     argInt(args, "message_thread_id"),
		MessageID:           argInt(args, "message_id"),
		UserID:              argInt64(args, "user_id"),
		Name:                argString(args, "name"),
		Text:                argString(args, "text"),
		InviteLink:          argString(args, "invite_link"),
		IconColor:           argInt(args, "icon_color"),
		IconCustomEmojiID:   argString(args, "icon_custom_emoji_id"),
		ExpireDate:          argInt64(args, "expire_date"),
		MemberLimit:         argInt(args, "member_limit"),
		DisableNotification: argBool(args, "disable_notification"),
		CreatesJoinRequest:  argBool(args, "creates_join_request"),
		OnlyIfBanned:        argBool(args, "only_if_banned"),
		RevokeMessages:      argBool(args, "revoke_messages"),
	}
	if rawParams, ok := args["params"].(map[string]any); ok {
		req.Params = rawParams
	}

	if err := validateTelegramManagerRequest(req); err != nil {
		return ErrorResult(err.Error())
	}

	result, err := t.manager(ctx, channel, req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("telegram_manager %s failed: %v", action, err))
	}
	if result.Action == "" {
		result.Action = action
	}

	data, _ := json.Marshal(map[string]any{
		"ok":      true,
		"channel": channel,
		"chat_id": req.ChatID,
		"action":  result.Action,
		"result":  result.Result,
	})
	return NewResult(string(data))
}

var telegramManagerActions = map[string]struct{}{
	"chat.get":                         {},
	"chat.get_administrators":          {},
	"chat.get_member_count":            {},
	"chat.get_member":                  {},
	"chat.set_title":                   {},
	"chat.set_description":             {},
	"chat.leave":                       {},
	"member.ban":                       {},
	"member.unban":                     {},
	"member.set_custom_title":          {},
	"join_request.approve":             {},
	"join_request.decline":             {},
	"invite.export":                    {},
	"invite.create":                    {},
	"invite.revoke":                    {},
	"message.delete":                   {},
	"message.pin":                      {},
	"message.unpin":                    {},
	"message.unpin_all":                {},
	"topic.get_icon_stickers":          {},
	"topic.create":                     {},
	"topic.edit":                       {},
	"topic.close":                      {},
	"topic.reopen":                     {},
	"topic.delete":                     {},
	"topic.unpin_all_messages":         {},
	"topic.general.edit":               {},
	"topic.general.close":              {},
	"topic.general.reopen":             {},
	"topic.general.hide":               {},
	"topic.general.unhide":             {},
	"topic.general.unpin_all_messages": {},
}

var telegramManagerPermissionGroups = map[string]struct{}{
	"chat":         {},
	"member":       {},
	"join_request": {},
	"invite":       {},
	"message":      {},
	"topic":        {},
}

func telegramManagerPermissionGroup(action string) string {
	prefix, _, _ := strings.Cut(action, ".")
	if prefix == "join_request" {
		return "join_request"
	}
	if _, ok := telegramManagerPermissionGroups[prefix]; ok {
		return prefix
	}
	return ""
}

func telegramManagerPermissionAllowed(permissions []string, group string) bool {
	for _, permission := range permissions {
		if strings.EqualFold(strings.TrimSpace(permission), group) {
			return true
		}
	}
	return false
}

func validateTelegramManagerRequest(req channels.TelegramManagerRequest) error {
	switch req.Action {
	case "topic.get_icon_stickers":
		return nil
	case "chat.get", "chat.get_administrators", "chat.get_member_count", "message.unpin_all",
		"chat.leave", "invite.export", "invite.create",
		"topic.general.close", "topic.general.reopen", "topic.general.hide", "topic.general.unhide", "topic.general.unpin_all_messages":
		return requireChatID(req)
	case "chat.get_member", "member.ban", "member.unban", "join_request.approve", "join_request.decline":
		if err := requireChatID(req); err != nil {
			return err
		}
		if req.UserID == 0 {
			return fmt.Errorf("user_id is required for %s", req.Action)
		}
		return nil
	case "member.set_custom_title":
		if err := requireChatID(req); err != nil {
			return err
		}
		if req.UserID == 0 {
			return fmt.Errorf("user_id is required for %s", req.Action)
		}
		if strings.TrimSpace(req.Text) == "" {
			return fmt.Errorf("text is required for %s", req.Action)
		}
		return nil
	case "chat.set_title", "chat.set_description":
		if err := requireChatID(req); err != nil {
			return err
		}
		if strings.TrimSpace(req.Text) == "" {
			return fmt.Errorf("text is required for %s", req.Action)
		}
		return nil
	case "invite.revoke":
		if err := requireChatID(req); err != nil {
			return err
		}
		if strings.TrimSpace(req.InviteLink) == "" {
			return fmt.Errorf("invite_link is required for %s", req.Action)
		}
		return nil
	case "message.delete", "message.pin":
		if err := requireChatID(req); err != nil {
			return err
		}
		if req.MessageID == 0 {
			return fmt.Errorf("message_id is required for %s", req.Action)
		}
		return nil
	case "message.unpin":
		return requireChatID(req)
	case "topic.create", "topic.general.edit":
		if err := requireChatID(req); err != nil {
			return err
		}
		if strings.TrimSpace(req.Name) == "" {
			return fmt.Errorf("name is required for %s", req.Action)
		}
		return nil
	case "topic.edit", "topic.close", "topic.reopen", "topic.delete", "topic.unpin_all_messages":
		if err := requireChatID(req); err != nil {
			return err
		}
		if req.MessageThreadID == 0 {
			return fmt.Errorf("message_thread_id is required for %s", req.Action)
		}
		return nil
	default:
		return fmt.Errorf("unsupported telegram_manager action: %s", req.Action)
	}
}

func requireChatID(req channels.TelegramManagerRequest) error {
	if strings.TrimSpace(req.ChatID) == "" {
		return fmt.Errorf("chat_id is required for %s (no current chat in context)", req.Action)
	}
	return nil
}

func argInt(m map[string]any, key string) int { return int(argInt64(m, key)) }

func argInt64(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		parsed, _ := n.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return parsed
	default:
		return 0
	}
}

func argBool(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(b))
		return parsed
	default:
		return false
	}
}
