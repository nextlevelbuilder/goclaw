package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// embeddedMediaPattern matches "MEDIA:" followed by a non-whitespace path.
// Duplicated from agent.mediaPathPattern to avoid tools→agent import cycle.
var embeddedMediaPattern = regexp.MustCompile(`MEDIA:\S+`)

// MessageTool allows the agent to proactively send messages to channels.
type MessageTool struct {
	workspace     string
	restrict      bool
	sender        ChannelSender
	msgBus        *bus.MessageBus
	tenantChecker ChannelTenantChecker
}

func NewMessageTool(workspace string, restrict bool) *MessageTool {
	return &MessageTool{workspace: workspace, restrict: restrict}
}

func (t *MessageTool) SetChannelSender(s ChannelSender)               { t.sender = s }
func (t *MessageTool) SetMessageBus(b *bus.MessageBus)                { t.msgBus = b }
func (t *MessageTool) SetChannelTenantChecker(c ChannelTenantChecker) { t.tenantChecker = c }

func (t *MessageTool) Name() string { return "message" }
func (t *MessageTool) Description() string {
	return "Send a message to a channel (Telegram, Discord, Slack, Zalo, Feishu/Lark, WhatsApp, etc.). In a DM/group, omit `target` to reply to the current chat — DO NOT set a different target unless the user explicitly asked you to forward (then set `forward=true` + `forward_reason` quoting the request). In cron/heartbeat/subagent/team contexts, set `target` per job spec."
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
			"forward": map[string]any{
				"type":        "boolean",
				"description": "Set true ONLY when the user explicitly asked to forward to a different chat than the current one. Required when target ≠ current chat in DM/group sessions.",
			},
			"forward_reason": map[string]any{
				"type":        "string",
				"description": "Quote the user's literal request when forward=true (e.g. 'gửi báo cáo này sang group dev'). Required when forward=true.",
			},
		},
		"required": []string{"action", "message"},
	}
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]any) *Result {
	action := argString(args, "action")
	if action != "send" {
		return ErrorResult(fmt.Sprintf("unsupported action: %s (only 'send' is supported)", action))
	}

	message := argString(args, "message")
	if message == "" {
		return ErrorResult("message is required")
	}

	channel := argString(args, "channel")
	if channel == "" {
		channel = ToolChannelFromCtx(ctx)
	}
	if channel == "" {
		return ErrorResult("channel is required (no current channel in context)")
	}

	target := argString(args, "target")
	if target == "" {
		target = ToolChatIDFromCtx(ctx)
	}
	if target == "" {
		return ErrorResult("target chat ID is required (no current chat in context)")
	}

	// Self-send guard: prevent agent from sending to its own chat via message tool.
	// Text self-sends are always blocked (response goes through normal outbound).
	// MEDIA self-sends are allowed ONLY when the file was NOT already queued for
	// delivery (i.e. write_file was called with deliver=false). This prevents both
	// duplicate delivery (deliver=true then message MEDIA:) and runaway retry loops
	// (deliver=false then message MEDIA: blocked unconditionally).
	ctxChannel := ToolChannelFromCtx(ctx)
	ctxChatID := ToolChatIDFromCtx(ctx)
	isSelfSend := ctxChannel != "" && ctxChatID != "" && channel == ctxChannel && target == ctxChatID
	if isSelfSend {
		isMediaSend := embeddedMediaPattern.MatchString(message)
		if !isMediaSend {
			return ErrorResult("You are already responding to this chat. Your response text will be delivered automatically. Do not use the message tool to send text to your own chat — just include the content in your response text. To deliver files, use write_file with deliver=true instead.")
		}
		// MEDIA self-send: block if ALL referenced files are already queued for delivery.
		// Extracts paths from both standalone "MEDIA:path" and embedded multi-line messages.
		if dm := DeliveredMediaFromCtx(ctx); dm != nil {
			mediaRefs := embeddedMediaPattern.FindAllString(message, -1)
			allDelivered := len(mediaRefs) > 0
			for _, raw := range mediaRefs {
				if filePath, ok := t.resolveMediaPath(ctx, raw); ok {
					if !dm.IsDelivered(filePath) {
						allDelivered = false
						break
					}
				}
			}
			if allDelivered {
				return ErrorResult("This file is already queued for automatic delivery via write_file(deliver=true). Do not send it again. To deliver files that were written with deliver=false, use write_file again with deliver=true, or use message(MEDIA:path) which is allowed for undelivered files.")
			}
		}
	}

	// Tenant isolation: validate channel belongs to current tenant.
	if err := t.validateChannelTenant(ctx, channel, target); err != nil {
		return err
	}

	// Cross-target guard: in DM/group/default sessions, prevent the agent from
	// sending to a chat other than the one bound to its context. FREE session
	// kinds (cron/heartbeat/subagent/team) compose targets per job spec and
	// bypass this guard. Opt-in forwarding requires forward=true +
	// non-empty forward_reason; notice is posted back to the origin chat and
	// an slog audit line is emitted.
	sessionKey := ToolSessionKeyFromCtx(ctx)
	var forwardReason string // non-empty ⇒ guard allowed a cross-target forward; post notice on success
	if MessageTargetEnforced(sessionKey) {
		crossTarget := channel != ctxChannel || target != ctxChatID
		if crossTarget && (ctxChannel != "" || ctxChatID != "") {
			forward, _ := args["forward"].(bool)
			reason := strings.TrimSpace(argString(args, "forward_reason"))
			if !forward || reason == "" {
				return ErrorResult(fmt.Sprintf(
					"Cross-target send blocked. You are bound to %s/%s but tried to send to %s/%s. "+
						"If the user explicitly asked you to forward, retry with forward=true AND forward_reason=\"<quote user's literal request>\".",
					ctxChannel, ctxChatID, channel, target))
			}
			slog.Warn("message.cross_target_forward",
				"session", sessionKey,
				"from_channel", ctxChannel, "from", ctxChatID,
				"to_channel", channel, "to", target,
				"reason", reason)
			forwardReason = reason
		}
	}
	// noticeOnSuccess posts the cross-target breadcrumb back to origin iff the
	// forward succeeded (res.IsError == false). Guarantees we never announce
	// a fake delivery when the downstream sender/bus publish fails.
	noticeOnSuccess := func(res *Result) *Result {
		if forwardReason != "" && res != nil && !res.IsError {
			t.postCrossTargetNotice(ctx, target, forwardReason)
		}
		return res
	}

	// Handle MEDIA: prefix — send file as attachment instead of text.
	if filePath, ok := t.resolveMediaPath(ctx, message); ok {
		return noticeOnSuccess(t.sendMedia(ctx, channel, target, filePath))
	}

	// Extract embedded MEDIA: paths from multi-line messages.
	// LLMs may include MEDIA: in conversational text rather than as a standalone prefix.
	message, embeddedMedia := t.extractEmbeddedMedia(ctx, message)

	// If we found embedded media and bus is available, prefer bus path (supports media attachments).
	if len(embeddedMedia) > 0 && t.msgBus != nil {
		outMsg := bus.OutboundMessage{
			Channel: channel,
			ChatID:  target,
			Content: message,
			Media:   embeddedMedia,
		}
		if isGroupContext(ctx) {
			outMsg.Metadata = map[string]string{"group_id": target}
		}
		t.msgBus.PublishOutbound(outMsg)
		// Mark each embedded media path as delivered.
		if dm := DeliveredMediaFromCtx(ctx); dm != nil {
			for _, att := range embeddedMedia {
				dm.Mark(att.URL)
			}
		}
		return noticeOnSuccess(SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target)))
	}

	// Prefer direct channel sender for immediate delivery.
	// For group chats, fall through to message bus which supports metadata.
	if t.sender != nil && !isGroupContext(ctx) {
		if err := t.sender(ctx, channel, target, message); err != nil {
			return ErrorResult(fmt.Sprintf("failed to send message: %v", err))
		}
		return noticeOnSuccess(SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target)))
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
		return noticeOnSuccess(SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target)))
	}

	// Last resort: direct sender without group metadata.
	if t.sender != nil {
		if err := t.sender(ctx, channel, target, message); err != nil {
			return ErrorResult(fmt.Sprintf("failed to send message: %v", err))
		}
		return noticeOnSuccess(SilentResult(fmt.Sprintf(`{"status":"sent","channel":"%s","target":"%s"}`, channel, target)))
	}

	return ErrorResult("no channel sender or message bus available")
}

// validateChannelTenant checks the target channel belongs to the current tenant.
// Returns an error Result if the send should be blocked, nil if allowed.
func (t *MessageTool) validateChannelTenant(ctx context.Context, channel, target string) *Result {
	if t.tenantChecker == nil {
		return nil
	}
	chTenant, chExists := t.tenantChecker(channel)
	if !chExists {
		return ErrorResult(fmt.Sprintf("channel %q not found", channel))
	}
	// Allow: legacy/config-based channels (zero tenant) or master tenant context (system ops).
	if chTenant == uuid.Nil {
		return nil
	}
	ctxTenant := store.TenantIDFromContext(ctx)
	if ctxTenant == uuid.Nil {
		return nil // master tenant / system context
	}
	if chTenant != ctxTenant {
		slog.Warn("security.cross_tenant_send_blocked",
			"channel", channel, "target", target,
			"ctx_tenant", ctxTenant, "ch_tenant", chTenant)
		return ErrorResult("channel not accessible from this tenant")
	}
	return nil
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
		Media:    []bus.MediaAttachment{{URL: filePath, ContentType: mimeFromPath(filePath)}},
		Metadata: meta,
	})
	// Mark delivered so subsequent send_file or message(MEDIA:) calls detect the duplicate.
	if dm := DeliveredMediaFromCtx(ctx); dm != nil {
		dm.Mark(filePath)
	}
	out, _ := json.Marshal(map[string]string{
		"status":  "sent",
		"channel": channel,
		"target":  target,
		"media":   filepath.Base(filePath),
	})
	return SilentResult(string(out))
}

// extractEmbeddedMedia scans a multi-line message for embedded MEDIA: path references.
// Returns cleaned text (MEDIA: lines removed) and resolved media attachments.
// Prevents raw MEDIA: paths from leaking to channels when LLMs embed them
// in conversational text instead of using a standalone MEDIA: prefix.
func (t *MessageTool) extractEmbeddedMedia(ctx context.Context, message string) (string, []bus.MediaAttachment) {
	if !strings.Contains(message, "MEDIA:") {
		return message, nil
	}

	lines := strings.Split(message, "\n")
	var cleaned []string
	var media []bus.MediaAttachment

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip [[audio_as_voice]] tags (TTS voice messages).
		if strings.HasPrefix(trimmed, "[[audio_as_voice]]") {
			continue
		}
		// Find all MEDIA: tokens on this line.
		matches := embeddedMediaPattern.FindAllString(trimmed, -1)
		if len(matches) == 0 {
			cleaned = append(cleaned, line)
			continue
		}
		// Extract each MEDIA: path and resolve via security-checked path resolution.
		for _, raw := range matches {
			if resolved, ok := t.resolveMediaPath(ctx, raw); ok {
				media = append(media, bus.MediaAttachment{
					URL:         resolved,
					ContentType: mimeFromPath(resolved),
				})
			}
		}
		// Strip MEDIA: tokens from line, keep surrounding text.
		remainder := strings.TrimSpace(embeddedMediaPattern.ReplaceAllString(line, ""))
		if remainder != "" {
			cleaned = append(cleaned, remainder)
		}
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n")), media
}

// mimeFromPath returns a MIME type based on file extension.
// Duplicated from agent.mimeFromExt to avoid tools→agent import cycle.
func mimeFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

// argString reads a tool argument as a non-empty string. LLM tool JSON often encodes
// numeric chat IDs as JSON numbers (float64); a plain .(string) type assert would
// ignore them and fall back to context — wrong for proactive sends to a group.
func argString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case float64:
		if s != s { // NaN
			return ""
		}
		// Telegram chat IDs are integers; json.Unmarshal uses float64 for all numbers.
		if s == float64(int64(s)) {
			return strconv.FormatInt(int64(s), 10)
		}
		return strings.TrimSpace(fmt.Sprintf("%.0f", s))
	case int:
		return strconv.FormatInt(int64(s), 10)
	case int64:
		return strconv.FormatInt(s, 10)
	case json.Number:
		return strings.TrimSpace(string(s))
	default:
		return strings.TrimSpace(fmt.Sprint(s))
	}
}

// isGroupContext returns true if the current context indicates a group conversation.
func isGroupContext(ctx context.Context) bool {
	userID := store.UserIDFromContext(ctx)
	return ToolPeerKindFromCtx(ctx) == "group" ||
		strings.HasPrefix(userID, "group:") ||
		strings.HasPrefix(userID, "guild:")
}

// resolveMediaPath extracts and validates a file path from a "MEDIA:path" string.
// Uses the same workspace-aware path resolution as other filesystem tools.
// Multi-tenant isolation forces MEDIA: paths through restricted resolution
// first, with one explicit fallback for generated media artifacts under /tmp/.
// In practice MEDIA: paths may resolve to:
//   - files inside the agent workspace
//   - absolute paths under /tmp/ for generated media artifacts
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
