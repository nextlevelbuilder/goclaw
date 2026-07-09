package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DingTalk conversation types, as sent on every inbound callback.
const (
	conversationTypeDirect = "1"
	conversationTypeGroup  = "2"
)

// dedupTTL bounds how long a message id is remembered. DingTalk redelivers on
// its own schedule; five minutes matches both the upstream connector and the
// Feishu channel.
const dedupTTL = 5 * time.Minute

// ackEmpty is what the SDK sends back to DingTalk. Returning from the handler
// *is* the ack, so the body is irrelevant — but it must not be nil.
var ackEmpty = []byte("")

// handleBotMessage is the SDK's inbound callback.
//
// Returning from this function is the ack DingTalk waits for. Everything past
// the dedup check therefore runs on its own goroutine: bus.PublishInbound is a
// bare channel send on a bounded buffer (internal/bus/bus.go:36), so a saturated
// bus would otherwise stall the socket's read loop and blow the ack deadline.
//
// Dedup runs synchronously and before the spawn. It is cheap, and a server
// resend that got past it would fork a second agent run for one user message.
func (c *Channel) handleBotMessage(_ context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	if data == nil {
		return ackEmpty, nil
	}
	if c.isDuplicate(data.MsgId) {
		slog.Debug("dingtalk duplicate message dropped",
			"channel", c.Name(), "msg_id", data.MsgId)
		return ackEmpty, nil
	}

	// The callback ctx dies with the frame. The agent run must not, so it
	// inherits the channel-scoped context captured in Start().
	c.stateMu.Lock()
	ctx := c.runCtx
	c.stateMu.Unlock()
	if ctx == nil {
		return ackEmpty, nil
	}

	go func() {
		defer safego.Recover(nil, "component", "dingtalk_inbound", "channel", c.Name())
		c.processMessage(ctx, data)
	}()

	return ackEmpty, nil
}

// isDuplicate reports whether msgID was already seen, and records it if not.
//
// Keyed on MsgId, which DingTalk keeps stable across its own resends. The
// per-delivery frame header id would be the more precise key, but the SDK does
// not surface it to the chatbot handler.
func (c *Channel) isDuplicate(msgID string) bool {
	if msgID == "" {
		return false
	}
	if _, loaded := c.dedup.LoadOrStore(msgID, struct{}{}); loaded {
		return true
	}
	time.AfterFunc(dedupTTL, func() { c.dedup.Delete(msgID) })
	return false
}

// mediaRef is an attachment DingTalk has not sent yet: downloadCode is redeemed
// for a signed URL at fetch time. Consumed in phase 5.
type mediaRef struct {
	Kind         string // picture | audio | video | file
	DownloadCode string
	FileName     string
}

// inbound is one parsed DingTalk message, normalized away from the wire shape.
type inbound struct {
	MsgType      string
	Text         string
	Media        []mediaRef
	MentionedBot bool

	SenderID   string
	SenderName string
	IsGroup    bool

	ConversationID string
	MessageID      string
	SessionWebhook string
	WebhookExpires time.Time
}

// parse normalizes a callback payload.
func parse(data *chatbot.BotCallbackDataModel) (*inbound, error) {
	in := &inbound{
		MsgType:        data.Msgtype,
		MessageID:      data.MsgId,
		SenderName:     data.SenderNick,
		IsGroup:        data.ConversationType == conversationTypeGroup,
		ConversationID: data.ConversationId,
		SessionWebhook: data.SessionWebhook,

		// In Stream mode the platform only delivers group messages in which the
		// bot was mentioned, but IsInAtList states it outright. The upstream
		// connector leans on the platform guarantee and never reads its own
		// requireMention setting; GoClaw promises to honor it, so it reads the flag.
		MentionedBot: data.IsInAtList,
	}

	// senderStaffId is the corp-scoped user id and the one every other DingTalk
	// API expects. senderId is a conversation-scoped fallback.
	in.SenderID = data.SenderStaffId
	if in.SenderID == "" {
		in.SenderID = data.SenderId
	}

	if data.SessionWebhookExpiredTime > 0 {
		in.WebhookExpires = time.UnixMilli(data.SessionWebhookExpiredTime)
	}

	if err := in.extractContent(data); err != nil {
		return nil, err
	}
	return in, nil
}

// extractContent pulls text and attachment handles out of the payload.
func (in *inbound) extractContent(data *chatbot.BotCallbackDataModel) error {
	switch data.Msgtype {
	case "text":
		in.Text = strings.TrimSpace(data.Text.Content)
		return nil

	case "markdown":
		// Markdown arrives in text.content, not content. Reading data.Content
		// here yields an empty message with no error — the connector carries a
		// comment about exactly this.
		in.Text = strings.TrimSpace(data.Text.Content)
		return nil
	}

	raw, err := rawContent(data.Content)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}

	switch data.Msgtype {
	case "richText":
		return in.extractRichText(raw)

	case "picture":
		var c struct {
			DownloadCode string `json:"downloadCode"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("dingtalk: decode picture content: %w", err)
		}
		in.addMedia("picture", c.DownloadCode, "image.jpg")
		return nil

	case "audio":
		var c struct {
			DownloadCode string `json:"downloadCode"`
			Recognition  string `json:"recognition"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("dingtalk: decode audio content: %w", err)
		}
		// DingTalk transcribes voice notes for us. Feishu has no equivalent and
		// must run STT on every clip.
		in.Text = strings.TrimSpace(c.Recognition)
		in.addMedia("audio", c.DownloadCode, "audio.amr")
		return nil

	case "video":
		var c struct {
			DownloadCode string `json:"downloadCode"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("dingtalk: decode video content: %w", err)
		}
		in.addMedia("video", c.DownloadCode, "video.mp4")
		return nil

	case "file":
		var c struct {
			DownloadCode string `json:"downloadCode"`
			FileName     string `json:"fileName"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return fmt.Errorf("dingtalk: decode file content: %w", err)
		}
		in.addMedia("file", c.DownloadCode, c.FileName)
		return nil

	default:
		// actionCard, interactiveCard, reply, and anything DingTalk adds later.
		// Unknown types are dropped rather than guessed at.
		return nil
	}
}

// extractRichText handles both rich-text shapes. The newer payload nests the
// item list under "richText"; the older one under "richText.richTextList".
func (in *inbound) extractRichText(raw []byte) error {
	type item struct {
		Text         string `json:"text"`
		Type         string `json:"type"`
		DownloadCode string `json:"downloadCode"`
	}
	var c struct {
		RichText     []item `json:"richText"`
		RichTextList []item `json:"richTextList"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("dingtalk: decode richText content: %w", err)
	}

	items := c.RichText
	if len(items) == 0 {
		items = c.RichTextList
	}

	var text strings.Builder
	for _, it := range items {
		if it.Text != "" {
			text.WriteString(it.Text)
		}
		if it.DownloadCode != "" {
			kind := it.Type
			if kind == "" {
				kind = "file"
			}
			in.addMedia(kind, it.DownloadCode, "")
		}
	}
	in.Text = strings.TrimSpace(text.String())
	return nil
}

func (in *inbound) addMedia(kind, downloadCode, fileName string) {
	if downloadCode == "" {
		return
	}
	in.Media = append(in.Media, mediaRef{Kind: kind, DownloadCode: downloadCode, FileName: fileName})
}

// rawContent normalizes data.Content, which DingTalk sends as a JSON object on
// some message types and as a JSON *string* holding that object on others.
// Returns nil when there is no content at all.
func rawContent(v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, nil
		}
		return []byte(t), nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("dingtalk: re-encode content: %w", err)
		}
		return raw, nil
	}
}

// chatID is the conversation identity GoClaw routes on.
//
// group_session_scope=group_sender isolates each speaker inside a group, which
// is what an assistant shared by a busy team usually wants.
func (c *Channel) chatID(in *inbound) string {
	if !in.IsGroup {
		return in.SenderID
	}
	if c.cfg.GroupSessionScope == GroupSessionScopeGroupSender {
		return in.ConversationID + ":" + in.SenderID
	}
	return in.ConversationID
}

// processMessage runs the policy gates and publishes to the bus. It runs on its
// own goroutine, after the ack.
func (c *Channel) processMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) {
	ctx = store.WithTenantID(ctx, c.TenantID())

	in, err := parse(data)
	if err != nil {
		slog.Warn("dingtalk parse failed",
			"channel", c.Name(), "msg_id", data.MsgId, "msgtype", data.Msgtype, "error", err)
		return
	}
	if in.SenderID == "" {
		slog.Warn("dingtalk message without sender id", "channel", c.Name(), "msg_id", in.MessageID)
		return
	}

	chatID := c.chatID(in)
	senderName := channels.SanitizeDisplayName(in.SenderName)

	// CreateStream sees only a chatID; record what a card needs to be delivered.
	c.rememberChat(chatID, chatMeta{
		ConversationID: in.ConversationID,
		UserID:         in.SenderID,
		IsGroup:        in.IsGroup,
	})

	if in.IsGroup && !c.checkGroupPolicy(ctx, in, chatID) {
		return
	}
	if !in.IsGroup && !c.checkDMPolicy(ctx, in) {
		return
	}

	// Attachments are fetched before the mention gate so a file dropped into a
	// group without an @ still reaches the agent when someone does @ later.
	mediaInfos := c.resolveMedia(ctx, in)

	if in.IsGroup && c.RequireMention() && !in.MentionedBot {
		c.GroupHistory().Record(chatID, channels.HistoryEntry{
			Sender:    senderName,
			SenderID:  in.SenderID,
			Body:      in.Text,
			Media:     mediaPaths(mediaInfos),
			Timestamp: time.Now(),
			MessageID: in.MessageID,
		}, c.HistoryLimit())
		slog.Debug("dingtalk group message recorded without reply; bot not mentioned",
			"channel", c.Name(), "chat_id", chatID, "msg_id", in.MessageID)
		return
	}

	// A message with neither text nor a usable attachment has nothing to say.
	if in.Text == "" && len(mediaInfos) == 0 {
		return
	}

	content := in.Text
	if tags := media.BuildMediaTags(mediaInfos); tags != "" {
		if content == "" {
			content = tags
		} else {
			content = content + "\n" + tags
		}
	}

	peerKind := "direct"
	if in.IsGroup {
		peerKind = "group"
		if senderName != "" {
			content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
		}
		content = c.GroupHistory().BuildContext(chatID, content, c.HistoryLimit())
	}

	published := c.publish(ctx, bus.InboundMessage{
		Channel:      c.Name(),
		SenderID:     in.SenderID,
		ChatID:       chatID,
		Content:      content,
		Media:        mediaFiles(mediaInfos),
		PeerKind:     peerKind,
		UserID:       in.SenderID,
		AgentID:      c.AgentID(),
		HistoryLimit: c.HistoryLimit(),
		TenantID:     c.TenantID(),
		Metadata:     c.buildMetadata(in),
	})
	if !published {
		return
	}

	if in.IsGroup {
		c.GroupHistory().Clear(chatID)
	}
}

// publishRetryInterval paces retries while the inbound bus is saturated.
const publishRetryInterval = 50 * time.Millisecond

// publish enqueues an inbound message, giving up if the channel stops.
//
// bus.PublishInbound is a bare channel send that ignores context, so a saturated
// bus would pin this goroutine forever — and at shutdown, forever means leaked.
// Retrying TryPublishInbound keeps the backpressure while staying interruptible.
func (c *Channel) publish(ctx context.Context, msg bus.InboundMessage) bool {
	for {
		if c.Bus().TryPublishInbound(msg) {
			return true
		}
		select {
		case <-ctx.Done():
			slog.Warn("dingtalk inbound dropped; bus saturated and channel stopping",
				"channel", c.Name(), "chat_id", msg.ChatID)
			return false
		case <-time.After(publishRetryInterval):
		}
	}
}

// mediaFiles converts resolved media into the bus representation, preserving the
// MIME type the agent's media tools dispatch on.
func mediaFiles(infos []media.MediaInfo) []bus.MediaFile {
	if len(infos) == 0 {
		return nil
	}
	out := make([]bus.MediaFile, 0, len(infos))
	for _, m := range infos {
		out = append(out, bus.MediaFile{
			Path:     m.FilePath,
			MimeType: m.ContentType,
			Filename: m.FileName,
		})
	}
	return out
}

// mediaPaths lists file paths for the group-history record.
func mediaPaths(infos []media.MediaInfo) []string {
	if len(infos) == 0 {
		return nil
	}
	out := make([]string, 0, len(infos))
	for _, m := range infos {
		out = append(out, m.FilePath)
	}
	return out
}

// buildMetadata carries the DingTalk-specific handles the outbound path needs.
//
// The dingtalk_* keys are the ones that must survive the hop into
// OutboundMessage.Metadata, and so are listed in channels.routingMetaKeys. The
// unprefixed keys are for logging and other channels' shared conventions.
func (c *Channel) buildMetadata(in *inbound) map[string]string {
	chatType := "direct"
	if in.IsGroup {
		chatType = "group"
	}

	md := map[string]string{
		"platform":      channels.TypeDingtalk,
		"message_id":    in.MessageID,
		"sender_id":     in.SenderID,
		"sender_name":   in.SenderName,
		"display_name":  channels.SanitizeDisplayName(in.SenderName),
		"mentioned_bot": fmt.Sprintf("%t", in.MentionedBot),
		"chat_type":     chatType,

		// Routed to Send(). chat_type and conversation_id are both needed
		// because group_session_scope=group_sender suffixes the ChatID, so the
		// bare conversation id cannot be recovered from it.
		"dingtalk_chat_type":       chatType,
		"dingtalk_conversation_id": in.ConversationID,
	}

	// The session webhook is the cheap reply path, but it is scoped to this one
	// message and it expires. Send() falls back to the proactive OpenAPI.
	if in.SessionWebhook != "" {
		md["dingtalk_session_webhook"] = in.SessionWebhook
		if !in.WebhookExpires.IsZero() {
			md["dingtalk_webhook_expires_at"] = strconv.FormatInt(in.WebhookExpires.UnixMilli(), 10)
		}
	}
	return md
}
