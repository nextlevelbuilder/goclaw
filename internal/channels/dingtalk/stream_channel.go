package dingtalk

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

var _ channels.StreamingChannel = (*Channel)(nil)

// chatMeta is what CreateStream needs but a chatID cannot carry: which
// conversation to deliver a card into, and whether it is a group.
//
// group_session_scope=group_sender suffixes the chatID with the sender, so the
// conversation id genuinely cannot be recovered from the key.
type chatMeta struct {
	ConversationID string
	UserID         string
	IsGroup        bool
}

// rememberChat records the routing facts for a conversation, so a later
// CreateStream (which sees only a chatID) can address the card.
func (c *Channel) rememberChat(chatID string, meta chatMeta) {
	c.chatMeta.Store(chatID, meta)
}

func (c *Channel) lookupChat(chatID string) (chatMeta, bool) {
	v, ok := c.chatMeta.Load(chatID)
	if !ok {
		return chatMeta{}, false
	}
	return v.(chatMeta), true
}

// StreamEnabled reports whether this reply should stream into an AI Card.
//
// The asymmetry is deliberate and matches phase 4's replyMsgType: group_reply_mode
// governs groups only, so a DM keeps streaming even when groups are configured
// for plain text. async_mode disables streaming outright — it acks first and
// pushes a finished answer later, which is a different delivery model.
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if !c.cfg.StreamingOrDefault() || c.cfg.AsyncModeOrDefault() {
		return false
	}
	if isGroup && c.cfg.GroupReplyMode != GroupReplyModeAICard {
		return false
	}
	return true
}

// ReasoningStreamEnabled reports whether reasoning gets its own lane.
//
// False: the stock card template exposes a single msgContent key. A reasoning
// lane would need a second key registered in sys_full_json_obj.order, which is a
// template change, not a code change.
func (c *Channel) ReasoningStreamEnabled() bool { return false }

// CreateStream opens an AI Card for one run.
func (c *Channel) CreateStream(ctx context.Context, chatID string, _ bool) (channels.ChannelStream, error) {
	meta, ok := c.lookupChat(chatID)
	if !ok {
		// A cron or delegate run has no inbound message and therefore no
		// conversation metadata. Fall back to treating the chatID as a user id,
		// which is what a DM chatID already is.
		meta = chatMeta{UserID: chatID}
	}
	if meta.IsGroup && meta.ConversationID == "" {
		return nil, fmt.Errorf("dingtalk: no conversation id for group chat %q", chatID)
	}

	return c.createCard(ctx, meta)
}

// FinalizeStream hands the finished card to Send().
//
// Called only on run.completed. Send() then repaints the card with the final,
// formatted answer instead of posting a second message beneath it. On a failed
// or cancelled run this is not called, the card keeps whatever it streamed, and
// the framework's error message arrives as its own message — which is what a
// reader wants to see.
func (c *Channel) FinalizeStream(_ context.Context, chatID string, stream channels.ChannelStream) {
	cs, ok := stream.(*cardStream)
	if !ok {
		return
	}
	c.cards.Store(chatID, cs)
	slog.Debug("dingtalk card handed to Send for final repaint",
		"channel", c.Name(), "chat_id", chatID, "card", cs.outTrackID)
}

// takeCard claims the finished card for a chat, if a stream just ended there.
func (c *Channel) takeCard(chatID string) (*cardStream, bool) {
	v, ok := c.cards.LoadAndDelete(chatID)
	if !ok {
		return nil, false
	}
	return v.(*cardStream), true
}

// finalizeLiveCards drives every card still open to a terminal state.
//
// Called from Stop(). Without it, a gateway restart mid-run leaves cards
// spinning at INPUTING in the DingTalk UI with nothing left to finish them.
func (c *Channel) finalizeLiveCards(ctx context.Context) {
	c.liveCards.Range(func(_, v any) bool {
		cs, ok := v.(*cardStream)
		if !ok {
			return true
		}
		if err := cs.abort(ctx); err != nil {
			slog.Warn("dingtalk could not finalize card on shutdown",
				"channel", c.Name(), "card", cs.outTrackID, "error", err)
		}
		return true
	})
}
