package oa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// react_icon codes per Zalo OA v2.0 doc, in the order the Zalo client
// renders them in the reaction picker (haha → worry → cry → like → heart
// → angry → wow). /-remove is the retract sentinel, not a picker entry.
const (
	reactionIconHaha   = ":>"
	reactionIconWorry  = "--b"
	reactionIconCry    = ":-(("
	reactionIconLike   = "/-strong"
	reactionIconHeart  = "/-heart"
	reactionIconAngry  = ":-h"
	reactionIconWow    = ":o"
	reactionIconRemove = "/-remove"
)

// /-remove omitted: it's a control sentinel, not a status emoji the
// controller may resolve to.
var zaloSupportedReactions = map[string]bool{
	reactionIconHaha:  true,
	reactionIconWorry: true,
	reactionIconCry:   true,
	reactionIconLike:  true,
	reactionIconHeart: true,
	reactionIconAngry: true,
	reactionIconWow:   true,
}

func buildReactionBody(userID, sourceMessageID, reactIcon string) map[string]any {
	return map[string]any{
		"recipient": map[string]any{"user_id": userID},
		"sender_action": map[string]any{
			"react_icon":       reactIcon,
			"react_message_id": sourceMessageID,
		},
	}
}

// SendReaction bypasses c.post: reactions are best-effort and must not
// flip channel health on auth failure (no ForceRefresh, no MarkFailed).
func (c *Channel) SendReaction(ctx context.Context, userID, sourceMessageID, reactIcon string) (string, error) {
	if userID == "" || sourceMessageID == "" || reactIcon == "" {
		return "", errors.New("zalo_oa: SendReaction requires user_id, source message_id, react_icon")
	}
	tok, err := c.tokens.Access(ctx)
	if err != nil {
		return "", err
	}
	raw, err := c.client.apiPost(ctx, pathSendReaction,
		buildReactionBody(userID, sourceMessageID, reactIcon), tok)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Info().Family == FamilyPayload {
			slog.Warn("zalo_oa.reaction.dropped_payload_error",
				"oa_id", c.creds().OAID,
				"user_id", userID,
				"source_message_id", sourceMessageID,
				"icon", reactIcon,
				"zalo_code", apiErr.Code,
				"zalo_msg", apiErr.Message,
				"hint", "source message_id likely expired/deleted/over-50-cap")
		} else {
			slog.Debug("zalo_oa.reaction.send_failed",
				"oa_id", c.creds().OAID,
				"user_id", userID,
				"source_message_id", sourceMessageID,
				"icon", reactIcon,
				"error", err)
		}
		return "", err
	}
	mid, _ := parseMessageResponse(raw)
	slog.Debug("zalo_oa.reaction.sent",
		"oa_id", c.creds().OAID,
		"user_id", userID,
		"source_message_id", sourceMessageID,
		"icon", reactIcon,
		"message_id", mid)
	return mid, nil
}

func (c *Channel) SendClearReaction(ctx context.Context, userID, sourceMessageID string) error {
	if userID == "" || sourceMessageID == "" {
		return fmt.Errorf("zalo_oa: SendClearReaction requires user_id, source message_id")
	}
	_, err := c.SendReaction(ctx, userID, sourceMessageID, reactionIconRemove)
	return err
}
