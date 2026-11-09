package protocol

import (
	"encoding/json"
	"fmt"
)

// TextStyle is one Zalo text style range. Offsets are UTF-16 code units.
type TextStyle struct {
	Start int    `json:"start"`
	Len   int    `json:"len"`
	St    string `json:"st"`
}

// MentionInfo is one @mention range. Pos/Len are UTF-16 code units.
type MentionInfo struct {
	Pos  int    `json:"pos"`
	Len  int    `json:"len"`
	UID  string `json:"uid"`
	Type int    `json:"type"`
}

// SendOptions controls one outgoing text message.
type SendOptions struct {
	Text     string
	Styles   []TextStyle
	Mentions []MentionInfo
	Quote    *TMessage
	TTL      int
}

// addQuotePayload fills the qmsg* fields expected by Zalo for a quoted reply.
func addQuotePayload(payload map[string]any, quote *TMessage, threadType ThreadType) {
	if quote == nil {
		return
	}
	payload["qmsgOwner"] = quote.UIDFrom
	payload["qmsgId"] = quote.MsgID
	payload["qmsgCliId"] = quote.CliMsgID
	payload["qmsgType"] = clientMessageType(quote.MsgType)
	payload["qmsgTs"] = quote.TS
	payload["qmsgTTL"] = quote.TTL
	if quote.Content.String != nil {
		payload["qmsg"] = *quote.Content.String
	} else {
		payload["qmsg"] = ""
	}

	// Group replies carry the original attachment metadata so Zalo renders the
	// thumbnail; DM replies only need qmsg*. chat.todo keeps its raw shape.
	if threadType != ThreadTypeGroup || len(quote.Content.Raw) == 0 {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal(quote.Content.Raw, &obj); err != nil {
		return
	}
	if href, ok := obj["href"].(string); ok && href != "" {
		obj["oriUrl"] = href
		obj["normalUrl"] = href
	}
	if thumb, ok := obj["thumb"].(string); ok && thumb != "" {
		obj["thumbUrl"] = thumb
	}
	attach, err := json.Marshal(obj)
	if err != nil {
		return
	}
	payload["qmsgAttach"] = string(attach)
}

// clientMessageType maps inbound msgType strings to the integer Zalo expects
// in qmsgType. Unknown types use -1, matching the JS client.
func clientMessageType(msgType string) int {
	switch msgType {
	case "webchat", "chat.photo":
		return 1
	case "chat.sticker":
		return 2
	case "chat.voice":
		return 3
	case "chat.video", "chat.video.msg":
		return 4
	case "share.file":
		return 5
	case "chat.location.new":
		return 6
	case "chat.poll":
		return 7
	case "chat.card", "chat.bankcard":
		return 8
	case "chat.recommended":
		return 9
	case "chat.todo":
		return 10
	}
	return -1
}

func marshalJSONField(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal payload field: %w", err)
	}
	return string(b), nil
}
