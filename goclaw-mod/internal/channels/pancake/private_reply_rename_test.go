package pancake

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// TestPrivateReply_StatelessFiresEveryCall verifies private_reply fires on
// every Send() when Features.PrivateReply is enabled. Stateless design: no
// GoClaw-side dedup. Webhook-level comment_id dedup + FB per-comment
// idempotency handle duplicates; sender-level dedup intentionally removed.
func TestPrivateReply_StatelessFiresEveryCall(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.PrivateReply = true
	cfg.PrivateReplyMessage = "Hi {{commenter_name}}"
	ch, transport := newChannelWithMultiCapture(t, cfg)

	outMsg := bus.OutboundMessage{
		ChatID:  "conv-1",
		Content: "public reply",
		Metadata: map[string]string{
			"pancake_mode":        "comment",
			"sender_id":           "user-1",
			"reply_to_comment_id": "comment-1",
			"display_name":        "Tuan",
		},
	}

	if err := ch.Send(context.Background(), outMsg); err != nil {
		t.Fatalf("first Send: %v", err)
	}

	outMsg.ChatID = "conv-2"
	outMsg.Metadata["reply_to_comment_id"] = "comment-2"
	if err := ch.Send(context.Background(), outMsg); err != nil {
		t.Fatalf("second Send: %v", err)
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()

	var privateReplyCount int
	var lastBody string
	for _, body := range transport.bodies {
		var p map[string]any
		if err := json.Unmarshal(body, &p); err != nil {
			continue
		}
		if p["action"] == "private_reply" {
			privateReplyCount++
			if msg, _ := p["message"].(string); msg != "" {
				lastBody = msg
			}
		}
	}

	if privateReplyCount != 2 {
		t.Errorf("expected 2 private_reply calls (stateless, one per comment), got %d", privateReplyCount)
	}
	if lastBody != "Hi Tuan" {
		t.Errorf("private_reply body = %q, want %q (template should render)", lastBody, "Hi Tuan")
	}
}
