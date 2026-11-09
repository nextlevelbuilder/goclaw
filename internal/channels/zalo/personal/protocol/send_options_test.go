package protocol

import (
	"encoding/json"
	"testing"
)

func TestAddQuotePayloadTextAndGroupAttachment(t *testing.T) {
	text := "hello"
	quote := &TMessage{
		MsgID:    "m1",
		CliMsgID: "c1",
		UIDFrom:  "u1",
		TS:       "123",
		TTL:      3600,
		MsgType:  "webchat",
		Content:  Content{String: &text},
	}
	payload := map[string]any{}
	addQuotePayload(payload, quote, ThreadTypeUser)
	if payload["qmsgOwner"] != "u1" || payload["qmsgId"] != "m1" || payload["qmsgCliId"] != "c1" {
		t.Fatalf("quote ids = %#v", payload)
	}
	if payload["qmsgType"] != 1 || payload["qmsgTs"] != "123" || payload["qmsgTTL"] != 3600 {
		t.Fatalf("quote metadata = %#v", payload)
	}
	if payload["qmsg"] != "hello" {
		t.Fatalf("qmsg = %#v", payload["qmsg"])
	}
	if _, ok := payload["qmsgAttach"]; ok {
		t.Fatal("DM quote should not carry qmsgAttach")
	}
}

func TestAddQuotePayloadGroupAttachment(t *testing.T) {
	raw := json.RawMessage(`{"href":"https://example.com/a","thumb":"https://example.com/t.png"}`)
	quote := &TMessage{
		MsgID:   "m1",
		MsgType: "chat.photo",
		Content: Content{Raw: raw},
	}
	payload := map[string]any{}
	addQuotePayload(payload, quote, ThreadTypeGroup)
	attach, ok := payload["qmsgAttach"].(string)
	if !ok {
		t.Fatalf("qmsgAttach = %#v", payload["qmsgAttach"])
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(attach), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["oriUrl"] != "https://example.com/a" || obj["normalUrl"] != "https://example.com/a" {
		t.Fatalf("attachment urls = %#v", obj)
	}
	if obj["thumbUrl"] != "https://example.com/t.png" {
		t.Fatalf("thumbUrl = %#v", obj["thumbUrl"])
	}
	if payload["qmsgType"] != 1 {
		t.Fatalf("qmsgType = %#v", payload["qmsgType"])
	}
}

func TestClientMessageType(t *testing.T) {
	tests := map[string]int{
		"webchat":           1,
		"chat.photo":        1,
		"chat.sticker":      2,
		"chat.voice":        3,
		"chat.video":        4,
		"chat.video.msg":    4,
		"share.file":        5,
		"chat.location.new": 6,
		"chat.poll":         7,
		"chat.card":         8,
		"chat.bankcard":     8,
		"chat.recommended":  9,
		"chat.todo":         10,
		"unknown":           -1,
	}
	for msgType, want := range tests {
		if got := clientMessageType(msgType); got != want {
			t.Errorf("clientMessageType(%q) = %d, want %d", msgType, got, want)
		}
	}
}
