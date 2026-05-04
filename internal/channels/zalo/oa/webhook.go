package oa

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// oaInboundEvent maps a Zalo OA webhook event. Top-level "timestamp" is
// intentionally omitted — Zalo sends it as a string in real traffic;
// the signature verifier reads it independently via extractTimestamp.
type oaInboundEvent struct {
	EventName string `json:"event_name"`
	AppID     string `json:"app_id"`
	OAID      string `json:"oa_id"`
	Sender    struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name,omitempty"`
	} `json:"sender"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Message struct {
		MessageID   string         `json:"message_id,omitempty"`
		MsgID       string         `json:"msg_id,omitempty"` // alternate field in some OA payloads
		Text        string         `json:"text,omitempty"`
		Time        int64          `json:"time,omitempty"`
		Attachments []oaAttachment `json:"attachments,omitempty"`
	} `json:"message"`
}

func (e *oaInboundEvent) messageID() string {
	if e.Message.MessageID != "" {
		return e.Message.MessageID
	}
	return e.Message.MsgID
}

// HandleWebhookEvent routes a verified+deduped event onto the message bus.
// Drops self-echoes (Sender.ID == OAID). In bootstrap mode drops every
// event without decoding so Zalo's URL-save ping is acked but not
// dispatched.
func (c *Channel) HandleWebhookEvent(ctx context.Context, raw json.RawMessage) error {
	if c.inBootstrap() {
		n := c.bootstrapDroppedCount.Add(1)
		// Cap warn-level at first hit so a guessed slug can't amplify logs.
		if n == 1 {
			slog.Warn("zalo_oa.webhook.bootstrap_drop",
				"instance_id", c.instanceID,
				"drop_count", n,
				"hint", "paste OA Secret Key in Credentials tab to enable processing")
		} else {
			slog.Debug("zalo_oa.webhook.bootstrap_drop",
				"instance_id", c.instanceID, "drop_count", n)
		}
		return nil
	}
	var e oaInboundEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("zalo_oa.webhook: decode event: %w", err)
	}
	if e.Sender.ID != "" && e.Sender.ID == c.creds().OAID {
		slog.Debug("zalo_oa.webhook.self_echo_filtered",
			"oa_id", c.creds().OAID, "message_id", e.messageID())
		return nil
	}

	// Advance the per-sender cursor so a post-restart catch-up sweep skips
	// messages already delivered via webhook. Prefer message.time (matches
	// poll.go's cursor semantic); fall back to envelope timestamp only when
	// absent. Mixing message-time vs envelope-time would let envelope skew
	// over-advance the cursor and silently skip later messages on burst.
	if e.Sender.ID != "" {
		if e.Message.Time > 0 {
			c.cursor.Advance(e.Sender.ID, e.Message.Time)
		} else if ts, err := extractTimestamp(raw); err == nil && ts > 0 {
			c.cursor.Advance(e.Sender.ID, ts)
		}
	}

	switch e.EventName {
	case "user_send_text":
		c.dispatchWebhookText(&e)
		return nil
	case "user_send_image", "user_send_gif", "user_send_sticker":
		c.dispatchWebhookMedia(ctx, &e, true) // force image kind regardless of CDN MIME
		return nil
	case "user_send_file":
		c.dispatchWebhookMedia(ctx, &e, false)
		return nil
	case "user_send_link":
		c.dispatchWebhookLink(&e)
		return nil
	case "user_follow", "user_unfollow":
		slog.Info("zalo_oa.webhook.follow_event", "event", e.EventName, "user_id", e.Sender.ID)
		return nil
	case "oa_send_text", "oa_send_image", "oa_send_gif", "oa_send_sticker",
		"oa_send_file", "oa_send_link", "oa_send_list", "oa_send_request_user_info":
		// Name-match in case Zalo's payload shape change ever bypasses Sender.ID == OAID.
		slog.Debug("zalo_oa.webhook.outbound_mirror_dropped", "event", e.EventName)
		return nil
	default:
		slog.Debug("zalo_oa.webhook.unknown_event", "event", e.EventName)
		return nil
	}
}

func (c *Channel) dispatchWebhookText(e *oaInboundEvent) {
	if e.Message.Text == "" || e.Sender.ID == "" {
		return
	}
	metadata := common.InboundMeta{
		MessageID:         e.messageID(),
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: e.Sender.DisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(e.Sender.ID, e.Sender.ID, e.Message.Text, nil, metadata, "direct")
}

// SignatureVerifier returns a verifier bound to this channel's webhook
// secret + signature mode. Bootstrap mode accepts any payload so Zalo's
// URL-save ping returns 200; events are dropped in HandleWebhookEvent.
func (c *Channel) SignatureVerifier() common.SignatureVerifier {
	if c.inBootstrap() {
		return newOASignatureVerifier(c.creds().AppID, "", SignatureModeDisabled, 0)
	}
	return newOASignatureVerifier(
		c.creds().AppID,
		c.creds().WebhookSecretKey,
		c.cfg.WebhookSignatureMode,
		clampReplayWindowSeconds(c.cfg.WebhookReplayWindowSeconds),
	)
}

// MessageIDExtractor pulls the per-event id for the router's dedup.
// Empty id → router skips dedup; the streak counter watches for persistent
// emptiness as a schema-drift signal.
func (c *Channel) MessageIDExtractor() common.MessageIDExtractor {
	return oaMessageIDExtractor{}
}

type oaMessageIDExtractor struct{}

func (oaMessageIDExtractor) ExtractMessageID(raw json.RawMessage) string {
	var probe struct {
		Message struct {
			MessageID string `json:"message_id,omitempty"`
			MsgID     string `json:"msg_id,omitempty"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	if probe.Message.MessageID != "" {
		return probe.Message.MessageID
	}
	return probe.Message.MsgID
}
