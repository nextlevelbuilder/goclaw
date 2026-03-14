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
	workspace string
	restrict  bool
	sender    ChannelSender
	msgBus    *bus.MessageBus
}

func NewMessageTool(workspace string, restrict bool) *MessageTool {
	return &MessageTool{workspace: workspace, restrict: restrict}
}

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
				"description": "Message content to send. To send a file as attachment, use the prefix MEDIA: followed by the file path, e.g. 'MEDIA:docs/report.pdf' or 'MEDIA:/tmp/image.png'. The file will be uploaded as a document/photo/audio depending on its type.",
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
	if filePath, ok := t.resolveMediaPath(ctx, message); ok {
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

// resolveMediaPath extracts and validates a file path from a "MEDIA:path" string.
// Uses the same workspace-aware path resolution as other filesystem tools:
//   - When restrict_to_workspace is true: allows workspace dir + /tmp/
//   - When restrict_to_workspace is false: allows any valid path
//
// Relative paths are resolved against the agent's workspace.
func (t *MessageTool) resolveMediaPath(ctx context.Context, s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "MEDIA:") {
		return "", false
	}
	raw := strings.TrimSpace(s[len("MEDIA:"):])
	if raw == "" || raw == "." {
		return "", false
	}

	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	restrict := effectiveRestrict(ctx, t.restrict)

	// resolvePath handles relative→absolute, symlink, hardlink, boundary checks.
	resolved, err := resolvePath(raw, workspace, restrict)
	if err != nil {
		// When restricted, also allow /tmp/ paths (used by create_image, create_audio, etc.)
		// But reject paths that are siblings of the workspace — these are likely traversal
		// attacks where workspace/../X resolves inside /tmp/ because workspace itself is in /tmp/.
		cleaned := filepath.Clean(raw)
		wsParent := filepath.Dir(filepath.Clean(workspace))
		if restrict && isInTempDir(cleaned) && !isPathInside(cleaned, wsParent) {
			return cleaned, true
		}
		return "", false
	}

	return resolved, true
}

// isInTempDir checks whether an absolute path is inside os.TempDir().
func isInTempDir(path string) bool {
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return false
	}
	tmpDir := filepath.Clean(os.TempDir())
	return strings.HasPrefix(cleaned, tmpDir+string(filepath.Separator))
}
