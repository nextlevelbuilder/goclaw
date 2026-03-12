package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MessageTool allows the agent to proactively send messages to channels.
type MessageTool struct {
	sender ChannelSender
	msgBus *bus.MessageBus
}

func NewMessageTool() *MessageTool { return &MessageTool{} }

func (t *MessageTool) SetChannelSender(s ChannelSender) { t.sender = s }
func (t *MessageTool) SetMessageBus(b *bus.MessageBus)  { t.msgBus = b }

func (t *MessageTool) Name() string { return "message" }
func (t *MessageTool) Description() string {
	return "Send a message to a channel (Telegram, Discord, Slack, Zalo, Feishu/Lark, WhatsApp, etc.) or the current chat. Channel and target are auto-filled from context."
}

func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: 'send'",
				"enum":        []string{"send"},
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Channel name (default: current channel from context)",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Chat ID to send to (default: current chat from context)",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message content to send",
			},
		},
		"required": []string{"action", "message"},
	}
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action != "send" {
		return ErrorResult(fmt.Sprintf("unsupported action: %s (only 'send' is supported)", action))
	}

	message, _ := args["message"].(string)
	if message == "" {
		return ErrorResult("message is required")
	}

	channel, _ := args["channel"].(string)
	if channel == "" {
		channel = ToolChannelFromCtx(ctx)
	}
	if channel == "" {
		return ErrorResult("channel is required (no current channel in context)")
	}

	target, _ := args["target"].(string)
	if target == "" {
		target = ToolChatIDFromCtx(ctx)
	}
	if target == "" {
		return ErrorResult("target chat ID is required (no current chat in context)")
	}

	// Handle MEDIA: prefix — send file as attachment instead of text.
	if filePath, ok := parseMediaPath(message); ok {
		return t.sendMedia(ctx, channel, target, filePath)
	}

	// Prefer direct channel sender for immediate delivery.
	// For group chats, fall through to message bus which supports metadata.
	if t.sender != nil && !isGroupContext(ctx) {
		if err := t.sender(ctx, channel, target, message); err != nil {
			return ErrorResult(fmt.Sprintf("failed to send message: %v", err))
		}
		return SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target))
	}

	// Publish via message bus outbound queue.
	// Group messages include metadata so channel implementations (e.g. Zalo)
	// can distinguish group sends from DMs.
	if t.msgBus != nil {
		outMsg := bus.OutboundMessage{
			Channel: channel,
			ChatID:  target,
			Content: message,
		}
		if isGroupContext(ctx) {
			outMsg.Metadata = map[string]string{"group_id": target}
		}
		t.msgBus.PublishOutbound(outMsg)
		return SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target))
	}

	// Last resort: direct sender without group metadata.
	if t.sender != nil {
		if err := t.sender(ctx, channel, target, message); err != nil {
			return ErrorResult(fmt.Sprintf("failed to send message: %v", err))
		}
		return SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target))
	}

	return ErrorResult("no channel sender or message bus available")
}

// sendMedia sends a file as a media attachment via the outbound message bus.
func (t *MessageTool) sendMedia(ctx context.Context, channel, target, filePath string) *Result {
	if _, err := os.Stat(filePath); err != nil {
		return ErrorResult(fmt.Sprintf("file not found: %s", filePath))
	}
	if t.msgBus == nil {
		return ErrorResult("media sending requires message bus")
	}

	// Build metadata for group routing (Zalo needs group_id to choose group API).
	var meta map[string]string
	if isGroupContext(ctx) {
		meta = map[string]string{"group_id": target}
	}

	t.msgBus.PublishOutbound(bus.OutboundMessage{
		Channel:  channel,
		ChatID:   target,
		Media:    []bus.MediaAttachment{{URL: filePath}},
		Metadata: meta,
	})
	out, _ := json.Marshal(map[string]string{
		"status":  "sent",
		"channel": channel,
		"target":  target,
		"media":   filepath.Base(filePath),
	})
	return SilentResult(string(out))
}

// isGroupContext returns true if the current context indicates a group conversation.
func isGroupContext(ctx context.Context) bool {
	userID := store.UserIDFromContext(ctx)
	return ToolPeerKindFromCtx(ctx) == "group" ||
		strings.HasPrefix(userID, "group:") ||
		strings.HasPrefix(userID, "guild:")
}

// parseMediaPath extracts a file path from a "MEDIA:/path/to/file" string.
// Only allows absolute paths within os.TempDir() to prevent path traversal.
func parseMediaPath(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "MEDIA:") {
		return "", false
	}
	path := filepath.Clean(strings.TrimSpace(s[len("MEDIA:"):]))
	if path == "" || path == "." {
		return "", false
	}
	if !filepath.IsAbs(path) {
		return "", false
	}
	// Restrict to temp directory to prevent path traversal.
	tmpDir := filepath.Clean(os.TempDir())
	if !strings.HasPrefix(path, tmpDir+string(filepath.Separator)) && path != tmpDir {
		return "", false
	}
	return path, true
}
