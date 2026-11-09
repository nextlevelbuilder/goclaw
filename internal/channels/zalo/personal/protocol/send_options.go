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
// Mirrors zca-js prepareQMSG/prepareQMSGAttach.
func addQuotePayload(payload map[string]any, quote *TMessage, threadType ThreadType) {
	if quote == nil {
		return
	}
	if quote.Content.String == nil && (quote.MsgType == "webchat" || quote.MsgType == "group.poll") {
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
		payload["qmsg"] = todoQuoteText(quote)
	}

	// Group replies carry the original attachment metadata so Zalo renders the
	// thumbnail; DM replies only need qmsg*.
	if threadType != ThreadTypeGroup {
		return
	}
	if quote.Content.String != nil {
		if len(quote.PropertyExt) > 0 {
			payload["qmsgAttach"] = string(quote.PropertyExt)
		}
		return
	}
	if quote.MsgType == "chat.todo" {
		payload["qmsgAttach"] = `{"properties":{"color":0,"size":0,"type":0,"subType":0,"ext":"{\"shouldParseLinkOrContact\":0}"}}`
		return
	}
	if len(quote.Content.Raw) == 0 {
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

// todoQuoteText extracts the quoted todo item content (zca-js prepareQMSG).
func todoQuoteText(quote *TMessage) string {
	if quote.MsgType != "chat.todo" || len(quote.Content.Raw) == 0 {
		return ""
	}
	var content struct {
		Params string `json:"params"`
	}
	if err := json.Unmarshal(quote.Content.Raw, &content); err != nil || content.Params == "" {
		return ""
	}
	var params struct {
		Item struct {
			Content string `json:"content"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(content.Params), &params); err != nil {
		return ""
	}
	return params.Item.Content
}

// clientMessageType maps inbound msgType strings to the integer Zalo expects
// in qmsgType (zca-js getClientMessageType). Unknown types fall back to 1.
func clientMessageType(msgType string) int {
	switch msgType {
	case "webchat":
		return 1
	case "chat.voice":
		return 31
	case "chat.photo":
		return 32
	case "chat.sticker":
		return 36
	case "chat.doodle":
		return 37
	case "chat.recommended", "chat.link":
		return 38
	case "chat.location.new":
		return 43
	case "chat.video.msg":
		return 44
	case "share.file":
		return 46
	case "chat.gif":
		return 49
	}
	return 1
}

func marshalJSONField(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal payload field: %w", err)
	}
	return string(b), nil
}
