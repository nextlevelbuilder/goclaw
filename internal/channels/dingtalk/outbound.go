package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Proactive robot endpoints. Used when there is no live session webhook —
// cron-initiated messages, delegated replies, or a run that outlived the
// webhook's expiry.
const (
	pathRobotSendToUser  = "/v1.0/robot/oToMessages/batchSend"
	pathRobotSendToGroup = "/v1.0/robot/groupMessages/send"
)

// markdownTitleMaxLen bounds the title DingTalk shows in the conversation list.
const markdownTitleMaxLen = 24

// Send delivers an outbound message.
//
// Text is chunked, then each chunk is delivered independently: the session
// webhook when it is still alive, the proactive OpenAPI otherwise.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("dingtalk: channel %q is not running", c.Name())
	}
	if msg.ChatID == "" {
		return fmt.Errorf("dingtalk: outbound message has no chat id")
	}

	isGroup := msg.Metadata["dingtalk_chat_type"] == "group"
	msgType := c.replyMsgType(isGroup)

	if text := strings.TrimSpace(msg.Content); text != "" {
		for _, chunk := range channels.ChunkMarkdown(text, c.cfg.TextChunkLimit) {
			if err := c.deliver(ctx, msg, isGroup, msgType, chunk); err != nil {
				return err
			}
		}
	}

	// Media cannot ride the session webhook; sendMedia always uses the
	// proactive API.
	return c.sendMedia(ctx, msg, isGroup)
}

// replyMsgType picks the wire message type for a non-card reply.
//
// group_reply_mode is asymmetric on purpose: it only governs *groups*. A DM
// keeps the richer rendering (and, once phase 6 lands, the streaming card)
// regardless of how groups are configured. Reading the connector as "text mode
// turns cards off everywhere" is the easy mistake here.
func (c *Channel) replyMsgType(isGroup bool) string {
	if isGroup && c.cfg.GroupReplyMode == GroupReplyModeText {
		return msgTypeText
	}
	return msgTypeMarkdown
}

// deliver routes one chunk to whichever transport can carry it.
func (c *Channel) deliver(ctx context.Context, msg bus.OutboundMessage, isGroup bool, msgType, text string) error {
	webhook := msg.Metadata["dingtalk_session_webhook"]

	if webhook != "" && c.webhookUsable(msg) {
		err := c.replyWebhook(ctx, webhook, msgType, text)
		if err == nil {
			return nil
		}
		// A webhook can be rejected for reasons the expiry stamp cannot predict
		// (revoked, conversation archived). Falling back costs one token fetch
		// and turns a dropped reply into a delivered one.
		slog.Warn("dingtalk session webhook failed; falling back to proactive api",
			"channel", c.Name(), "chat_id", msg.ChatID, "error", err)
	}

	return c.sendProactive(ctx, msg, isGroup, msgType, text)
}

// webhookUsable reports whether the session webhook has not expired.
//
// A missing stamp means "unknown", and the connector simply uses the webhook in
// that case; so do we. An expired stamp is authoritative.
func (c *Channel) webhookUsable(msg bus.OutboundMessage) bool {
	stamp := msg.Metadata["dingtalk_webhook_expires_at"]
	if stamp == "" {
		return true
	}
	ms, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return true
	}
	return c.now().Before(time.UnixMilli(ms))
}

// sendProactive posts text through the robot OpenAPI, which works with no
// inbound message to reply to.
func (c *Channel) sendProactive(ctx context.Context, msg bus.OutboundMessage, isGroup bool, msgType, text string) error {
	msgKey, msgParam, err := buildMsgPayload(msgType, text)
	if err != nil {
		return err
	}
	return c.postProactive(ctx, msg, isGroup, msgKey, msgParam)
}

// postProactive posts a prepared msgKey/msgParam pair. Media and text share it.
func (c *Channel) postProactive(ctx context.Context, msg bus.OutboundMessage, isGroup bool, msgKey, msgParam string) error {
	var (
		path string
		body map[string]any
	)
	if isGroup {
		// group_session_scope=group_sender suffixes the ChatID, so the bare
		// conversation id has to come from metadata.
		conversationID := msg.Metadata["dingtalk_conversation_id"]
		if conversationID == "" {
			conversationID = msg.ChatID
		}
		path = pathRobotSendToGroup
		body = map[string]any{
			"robotCode":          c.cfg.ClientID,
			"openConversationId": conversationID,
			"msgKey":             msgKey,
			"msgParam":           msgParam,
		}
	} else {
		path = pathRobotSendToUser
		body = map[string]any{
			"robotCode": c.cfg.ClientID,
			"userIds":   []string{msg.ChatID},
			"msgKey":    msgKey,
			"msgParam":  msgParam,
		}
	}

	var out struct {
		ProcessQueryKey string `json:"processQueryKey"`
	}
	if err := c.client.doAPI(ctx, http.MethodPost, path, body, &out); err != nil {
		return err
	}
	// A 200 without processQueryKey means DingTalk accepted the call but queued
	// nothing. Silently returning nil here would report a delivered message that
	// never arrives.
	if out.ProcessQueryKey == "" {
		return fmt.Errorf("dingtalk: %s returned no processQueryKey", path)
	}
	return nil
}

// buildMsgPayload maps a message type to DingTalk's msgKey/msgParam pair.
//
// msgParam is a JSON-encoded *string*, not a nested object.
func buildMsgPayload(msgType, text string) (msgKey string, msgParam string, err error) {
	var param any
	switch msgType {
	case msgTypeMarkdown:
		msgKey = "sampleMarkdown"
		param = map[string]string{"title": markdownTitle(text), "text": text}
	default:
		msgKey = "sampleText"
		param = map[string]string{"content": text}
	}

	encoded, err := json.Marshal(param)
	if err != nil {
		return "", "", fmt.Errorf("dingtalk: encode msgParam: %w", err)
	}
	return msgKey, string(encoded), nil
}

// markdownTitle derives the conversation-list preview from the body. DingTalk
// requires a title and shows it instead of the text, so an empty one makes every
// message look blank in the sidebar.
func markdownTitle(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#> "))
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > markdownTitleMaxLen {
			return string(runes[:markdownTitleMaxLen]) + "…"
		}
		return line
	}
	return "Message"
}
