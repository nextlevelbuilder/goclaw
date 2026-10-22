package mattermost

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestExtractAttachments(t *testing.T) {
	text := "Build finished.\n\n```attachment\n{\"color\":\"good\",\"title\":\"CI #42\",\"fields\":[{\"title\":\"Tests\",\"value\":\"312 passed\",\"short\":true}]}\n```\n\nDetails below."
	rest, cards := extractAttachments(text)
	if rest != "Build finished.\n\n\n\nDetails below." {
		t.Fatalf("unexpected text: %q", rest)
	}
	if len(cards) != 1 || cards[0].Title != "CI #42" || cards[0].Color != "good" || len(cards[0].Fields) != 1 {
		t.Fatalf("unexpected cards: %+v", cards)
	}
	if cards[0].Fallback != "CI #42" {
		t.Fatalf("fallback must default to the title, got %q", cards[0].Fallback)
	}

	_, cards = extractAttachments("```attachment\n[{\"text\":\"a\"},{\"text\":\"b\"}]\n```")
	if len(cards) != 2 {
		t.Fatalf("array block must yield two cards, got %+v", cards)
	}
}

func TestExtractAttachments_KeepsInvalidBlocks(t *testing.T) {
	for _, text := range []string{
		"plain text",
		"```attachment\nnot json\n```",
		"```attachment\n{\"color\":\"good\"}\n```", // renders nothing
		"```json\n{\"title\":\"x\"}\n```",
		"inline ```attachment {\"title\":\"x\"}``` mention",
	} {
		rest, cards := extractAttachments(text)
		if rest != text || cards != nil {
			t.Errorf("extractAttachments(%q) = %q, %+v; want text unchanged", text, rest, cards)
		}
	}
}

func TestSend_AttachmentCardGoesUnderLastChunk(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)
	ch.placeholders.Store("dm1", "ph1")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "dm1",
		Content: "Deploy done.\n\n```attachment\n{\"color\":\"good\",\"title\":\"prod v1.2\"}\n```",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	patches := fs.callsTo("PUT", "/api/v4/posts/ph1/patch")
	if len(patches) != 1 || patches[0].Body["message"] != "Deploy done." {
		t.Fatalf("placeholder must get the text without the block, got %+v", patches)
	}
	props, _ := patches[0].Body["props"].(map[string]any)
	cards, _ := props["attachments"].([]any)
	if len(cards) != 1 || cards[0].(map[string]any)["title"] != "prod v1.2" {
		t.Fatalf("placeholder must carry the attachment card, got %+v", patches[0].Body)
	}
}

func TestSend_AttachmentOnlyReply(t *testing.T) {
	fs := newFakeServer(t)
	ch, _ := newTestChannel(t, fs, nil)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "c1",
		Content: "```attachment\n{\"title\":\"Status\",\"text\":\"all green\"}\n```",
		Metadata: map[string]string{
			"message_thread_id": "r1",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	posts := fs.callsTo("POST", "/api/v4/posts")
	if len(posts) != 1 || posts[0].Body["message"] != "" || posts[0].Body["root_id"] != "r1" {
		t.Fatalf("expected one card-only post in the thread, got %+v", posts)
	}
	if props, _ := posts[0].Body["props"].(map[string]any); props["attachments"] == nil {
		t.Fatalf("card-only post must carry attachments, got %+v", posts[0].Body)
	}
}
