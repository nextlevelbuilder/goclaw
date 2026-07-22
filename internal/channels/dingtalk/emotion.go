package dingtalk

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Emotion endpoints. DingTalk calls a reaction on a message an "emotion".
const (
	pathEmotionReply  = "/v1.0/robot/emotion/reply"
	pathEmotionRecall = "/v1.0/robot/emotion/recall"
)

// The one reaction this channel can post.
//
// DingTalk's emotion API is keyed by a numeric emotionId, and the only id whose
// meaning is documented anywhere reachable is the thinking face the upstream
// connector hardcodes. Guessing further ids would be fabrication, so
// reaction_level is a two-valued switch here rather than feishu's off/minimal/full
// — there is no second reaction for "minimal" to fall back to.
const (
	emotionType     = 2
	emotionID       = "2659900"
	emotionName     = "🤔思考中"
	emotionBGID     = "im_bg_1"
	emotionTimeout  = 5 * time.Second
	emotionTextName = emotionName
)

var _ channels.ReactionChannel = (*Channel)(nil)

// emotionState remembers which inbound messages carry our reaction, so a run
// that reports "thinking" then several tool statuses posts one reaction, not five.
type emotionState struct {
	posted sync.Map // messageID -> struct{}
}

// OnReactionEvent posts or recalls the 🤔 reaction on the user's own message as
// the agent run progresses.
//
// Reactions are cosmetic: every failure here is logged and swallowed. A run must
// never fail because an emoji did not stick.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID, messageID, status string) error {
	if !c.cfg.ReactionsEnabled() || messageID == "" {
		return nil
	}

	switch status {
	case "done", "error":
		return c.ClearReaction(ctx, chatID, messageID)
	}

	// Everything else — "thinking" and the per-tool statuses — is the same
	// in-progress state, because there is only one reaction to show.
	if _, loaded := c.emotions.posted.LoadOrStore(messageID, struct{}{}); loaded {
		return nil
	}
	if err := c.emotion(ctx, pathEmotionReply, chatID, messageID); err != nil {
		c.emotions.posted.Delete(messageID) // let a later status retry
		slog.Debug("dingtalk add reaction failed",
			"channel", c.Name(), "message_id", messageID, "error", err)
	}
	return nil
}

// ClearReaction recalls the reaction. Safe to call when none was posted.
func (c *Channel) ClearReaction(ctx context.Context, chatID, messageID string) error {
	if !c.cfg.ReactionsEnabled() || messageID == "" {
		return nil
	}
	if _, loaded := c.emotions.posted.LoadAndDelete(messageID); !loaded {
		return nil
	}
	if err := c.emotion(ctx, pathEmotionRecall, chatID, messageID); err != nil {
		slog.Debug("dingtalk recall reaction failed",
			"channel", c.Name(), "message_id", messageID, "error", err)
	}
	return nil
}

// emotion posts the add/recall request. Both take an identical body.
//
// openConversationId is required and cannot be derived from chatID: a DM's chatID
// is the sender's staff id, and group_session_scope=group_sender suffixes a
// group's. It comes from the metadata recorded when the message arrived.
func (c *Channel) emotion(ctx context.Context, path, chatID, messageID string) error {
	meta, ok := c.lookupChat(chatID)
	if !ok || meta.ConversationID == "" {
		return nil // a cron or delegate run has no inbound message to react to
	}

	ctx, cancel := context.WithTimeout(ctx, emotionTimeout)
	defer cancel()

	body := map[string]any{
		"robotCode":          c.cfg.ClientID,
		"openMsgId":          messageID,
		"openConversationId": meta.ConversationID,
		"emotionType":        emotionType,
		"emotionName":        emotionName,
		"textEmotion": map[string]string{
			"emotionId":    emotionID,
			"emotionName":  emotionName,
			"text":         emotionTextName,
			"backgroundId": emotionBGID,
		},
	}
	return c.client.doAPI(ctx, http.MethodPost, path, body, nil)
}
