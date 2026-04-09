package googlechat

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestParseEvent_Message(t *testing.T) {
	chatEvent := map[string]any{
		"type": "MESSAGE",
		"message": map[string]any{
			"name": "spaces/AAA/messages/BBB",
			"text": "hello bot",
			"sender": map[string]any{
				"name":        "users/12345",
				"displayName": "Test User",
			},
			"thread": map[string]any{
				"name": "spaces/AAA/threads/CCC",
			},
		},
		"space": map[string]any{
			"name": "spaces/AAA",
			"type": "DM",
		},
	}
	data, _ := json.Marshal(chatEvent)
	encoded := base64.StdEncoding.EncodeToString(data)

	evt, err := parseEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != "MESSAGE" {
		t.Errorf("type = %q, want MESSAGE", evt.Type)
	}
	if evt.SenderID != "users/12345" {
		t.Errorf("senderID = %q, want users/12345", evt.SenderID)
	}
	if evt.SpaceID != "spaces/AAA" {
		t.Errorf("spaceID = %q, want spaces/AAA", evt.SpaceID)
	}
	if evt.Text != "hello bot" {
		t.Errorf("text = %q, want 'hello bot'", evt.Text)
	}
	if evt.PeerKind != "direct" {
		t.Errorf("peerKind = %q, want direct", evt.PeerKind)
	}
	if evt.ThreadName != "spaces/AAA/threads/CCC" {
		t.Errorf("threadName = %q, want spaces/AAA/threads/CCC", evt.ThreadName)
	}
}

func TestParseEvent_GroupSpace(t *testing.T) {
	chatEvent := map[string]any{
		"type": "MESSAGE",
		"message": map[string]any{
			"text": "hey",
			"sender": map[string]any{
				"name": "users/999",
			},
		},
		"space": map[string]any{
			"name": "spaces/GGG",
			"type": "SPACE",
		},
	}
	data, _ := json.Marshal(chatEvent)
	encoded := base64.StdEncoding.EncodeToString(data)

	evt, err := parseEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if evt.PeerKind != "group" {
		t.Errorf("peerKind = %q, want group", evt.PeerKind)
	}
}

func TestParseEvent_AddedToSpace(t *testing.T) {
	chatEvent := map[string]any{
		"type": "ADDED_TO_SPACE",
		"space": map[string]any{
			"name": "spaces/AAA",
			"type": "DM",
		},
		"user": map[string]any{
			"name": "users/12345",
		},
	}
	data, _ := json.Marshal(chatEvent)
	encoded := base64.StdEncoding.EncodeToString(data)

	evt, err := parseEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != "ADDED_TO_SPACE" {
		t.Errorf("type = %q, want ADDED_TO_SPACE", evt.Type)
	}
}

func TestParseEvent_MalformedJSON(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("{bad json"))
	_, err := parseEvent(encoded)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseEvent_EmptyData(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(""))
	_, err := parseEvent(encoded)
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestParseEvent_MissingSender(t *testing.T) {
	chatEvent := map[string]any{
		"type":    "MESSAGE",
		"message": map[string]any{"text": "hello"},
		"space":   map[string]any{"name": "spaces/AAA", "type": "DM"},
	}
	data, _ := json.Marshal(chatEvent)
	encoded := base64.StdEncoding.EncodeToString(data)

	_, err := parseEvent(encoded)
	if err == nil {
		t.Fatal("expected error for missing sender")
	}
}

func TestParseEvent_BotSelfFilter(t *testing.T) {
	chatEvent := map[string]any{
		"type": "MESSAGE",
		"message": map[string]any{
			"text":   "bot reply",
			"sender": map[string]any{"name": "users/BOT123"},
		},
		"space": map[string]any{"name": "spaces/AAA", "type": "DM"},
	}
	data, _ := json.Marshal(chatEvent)
	encoded := base64.StdEncoding.EncodeToString(data)

	evt, err := parseEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if evt.SenderID != "users/BOT123" {
		t.Errorf("senderID = %q", evt.SenderID)
	}
}

func TestParseEvent_WithAttachment(t *testing.T) {
	chatEvent := map[string]any{
		"type": "MESSAGE",
		"message": map[string]any{
			"text":   "",
			"sender": map[string]any{"name": "users/12345"},
			"attachment": []any{
				map[string]any{
					"name":        "spaces/AAA/messages/BBB/attachments/CCC",
					"contentType": "image/png",
					"attachmentDataRef": map[string]any{
						"resourceName": "spaces/AAA/attachments/CCC",
					},
				},
			},
		},
		"space": map[string]any{"name": "spaces/AAA", "type": "DM"},
	}
	data, _ := json.Marshal(chatEvent)
	encoded := base64.StdEncoding.EncodeToString(data)

	evt, err := parseEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(evt.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(evt.Attachments))
	}
	if evt.Attachments[0].ResourceName != "spaces/AAA/attachments/CCC" {
		t.Errorf("resourceName = %q", evt.Attachments[0].ResourceName)
	}
}

func TestDedupCache(t *testing.T) {
	cache := newDedupCache(dedupTTL)

	if cache.seen("msg1") {
		t.Error("msg1 should not be seen yet")
	}
	cache.add("msg1")
	if !cache.seen("msg1") {
		t.Error("msg1 should be seen after add")
	}
	if !cache.seen("msg1") {
		t.Error("msg1 should still be seen")
	}
	if cache.seen("msg2") {
		t.Error("msg2 should not be seen")
	}
}
