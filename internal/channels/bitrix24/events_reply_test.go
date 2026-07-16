package bitrix24

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func parseForm(t *testing.T, v interface{ Encode() string }) *Event {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	return evt
}

func TestParseEvent_Form_ReplyMessage_Text(t *testing.T) {
	v := buildBitrixForm()
	v.Set("data[PARAMS][REPLY_MESSAGE][ID]", "315900")
	v.Set("data[PARAMS][REPLY_MESSAGE][AUTHOR_ID]", "62")
	v.Set("data[PARAMS][REPLY_MESSAGE][MESSAGE]", "báo giá cho anh nhé")

	rm := parseForm(t, v).Params.ReplyMessage
	if rm == nil {
		t.Fatal("ReplyMessage nil; want parsed")
	}
	if rm.ID != "315900" || rm.AuthorID != "62" || rm.Text != "báo giá cho anh nhé" {
		t.Errorf("ReplyMessage = %+v", rm)
	}
}

func TestParseEvent_Form_ReplyMessage_MediaEmptyText(t *testing.T) {
	// Media-only original: ID + AUTHOR present, MESSAGE empty (no file id inline).
	v := buildBitrixForm()
	v.Set("data[PARAMS][REPLY_MESSAGE][ID]", "315968")
	v.Set("data[PARAMS][REPLY_MESSAGE][AUTHOR_ID]", "62")
	v.Set("data[PARAMS][REPLY_MESSAGE][MESSAGE]", "")

	rm := parseForm(t, v).Params.ReplyMessage
	if rm == nil || rm.ID != "315968" || rm.Text != "" {
		t.Errorf("ReplyMessage = %+v; want ID 315968, empty text", rm)
	}
}

func TestParseEvent_Form_ReplyID_Fallback(t *testing.T) {
	// Only the nested PARAMS[REPLY_ID] present (no REPLY_MESSAGE[ID]).
	v := buildBitrixForm()
	v.Set("data[PARAMS][PARAMS][REPLY_ID]", "315901")

	rm := parseForm(t, v).Params.ReplyMessage
	if rm == nil || rm.ID != "315901" {
		t.Errorf("ReplyMessage = %+v; want fallback ID 315901", rm)
	}
}

func TestParseEvent_Form_NoReply_NilReplyMessage(t *testing.T) {
	if rm := parseForm(t, buildBitrixForm()).Params.ReplyMessage; rm != nil {
		t.Errorf("non-reply event should have nil ReplyMessage, got %+v", rm)
	}
}

func TestParseEvent_JSON_ReplyMessage(t *testing.T) {
	body := `{
		"event": "ONIMBOTMESSAGEADD",
		"data": {
			"PARAMS": {
				"MESSAGE_ID": "500",
				"DIALOG_ID": "chat9",
				"FROM_USER_ID": "7",
				"MESSAGE": "còn hàng không",
				"REPLY_MESSAGE": {"ID": 315900, "AUTHOR_ID": 62, "MESSAGE": "áo sơ mi trắng"}
			}
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	rm := evt.Params.ReplyMessage
	if rm == nil || rm.ID != "315900" || rm.AuthorID != "62" || rm.Text != "áo sơ mi trắng" {
		t.Errorf("ReplyMessage = %+v", rm)
	}
}

func TestParseEvent_JSON_ReplyID_Fallback(t *testing.T) {
	body := `{
		"event": "ONIMBOTMESSAGEADD",
		"data": {"PARAMS": {"MESSAGE_ID": "500", "FROM_USER_ID": "7", "PARAMS": {"REPLY_ID": 315901}}}
	}`
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if rm := evt.Params.ReplyMessage; rm == nil || rm.ID != "315901" {
		t.Errorf("ReplyMessage = %+v; want fallback ID 315901", rm)
	}
}
