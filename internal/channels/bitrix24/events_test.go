package bitrix24

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// buildBitrixForm returns a url.Values populated with a realistic
// ONIMBOTMESSAGEADD shape captured from a Bitrix portal. Individual tests
// override specific keys.
func buildBitrixForm() url.Values {
	v := url.Values{}
	v.Set("event", "ONIMBOTMESSAGEADD")
	v.Set("ts", "1713564321")
	// auth[*]
	v.Set("auth[domain]", "portal.bitrix24.com")
	v.Set("auth[application_token]", "APPSECRET")
	v.Set("auth[access_token]", "AT")
	v.Set("auth[refresh_token]", "RT")
	v.Set("auth[member_id]", "mem1")
	v.Set("auth[expires_in]", "3600")
	// data[PARAMS][*]
	v.Set("data[PARAMS][MESSAGE_ID]", "42")
	v.Set("data[PARAMS][DIALOG_ID]", "chat1234")
	v.Set("data[PARAMS][CHAT_ID]", "1234")
	v.Set("data[PARAMS][FROM_USER_ID]", "7")
	v.Set("data[PARAMS][TO_USER_ID]", "914")
	v.Set("data[PARAMS][MESSAGE]", "hello")
	v.Set("data[PARAMS][MESSAGE_TYPE]", "chat")
	// data[BOT][914][BOT_ID]
	v.Set("data[BOT][914][BOT_ID]", "914")
	return v
}

func TestParseEvent_FormURLEncoded_Minimal(t *testing.T) {
	v := buildBitrixForm()
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}

	if evt.Type != "ONIMBOTMESSAGEADD" {
		t.Errorf("Type = %q", evt.Type)
	}
	if evt.Auth.Domain != "portal.bitrix24.com" {
		t.Errorf("Auth.Domain = %q", evt.Auth.Domain)
	}
	if evt.Auth.AppToken != "APPSECRET" {
		t.Errorf("Auth.AppToken = %q", evt.Auth.AppToken)
	}
	if evt.Auth.MemberID != "mem1" {
		t.Errorf("Auth.MemberID = %q", evt.Auth.MemberID)
	}
	if evt.Auth.ExpiresIn != 3600 {
		t.Errorf("Auth.ExpiresIn = %d", evt.Auth.ExpiresIn)
	}
	if evt.Params.MessageID != "42" {
		t.Errorf("Params.MessageID = %q", evt.Params.MessageID)
	}
	if evt.Params.BotID != 914 {
		t.Errorf("Params.BotID = %d", evt.Params.BotID)
	}
	if evt.Params.DialogID != "chat1234" {
		t.Errorf("Params.DialogID = %q", evt.Params.DialogID)
	}
	if evt.Params.FromUserID != "7" {
		t.Errorf("Params.FromUserID = %q", evt.Params.FromUserID)
	}
	if evt.Params.Message != "hello" {
		t.Errorf("Params.Message = %q", evt.Params.Message)
	}
	if evt.Ts.Unix() != 1713564321 {
		t.Errorf("Ts = %v", evt.Ts)
	}
	if evt.Raw == nil {
		t.Errorf("Raw should be set for form inputs")
	}
}

func TestParseEvent_FormURLEncoded_WithFiles(t *testing.T) {
	v := buildBitrixForm()
	// First file (image)
	v.Set("data[PARAMS][FILES][0][id]", "f1")
	v.Set("data[PARAMS][FILES][0][name]", "cat.png")
	v.Set("data[PARAMS][FILES][0][type]", "image")
	v.Set("data[PARAMS][FILES][0][urlMachine]", "https://portal.bitrix24.com/disk/downloadFile/1/")
	v.Set("data[PARAMS][FILES][0][urlPreview]", "https://portal.bitrix24.com/disk/preview/1/")
	v.Set("data[PARAMS][FILES][0][size]", "12345")
	v.Set("data[PARAMS][FILES][0][mime]", "image/png")
	// Second file (voice)
	v.Set("data[PARAMS][FILES][1][name]", "voice.ogg")
	v.Set("data[PARAMS][FILES][1][type]", "audio")
	v.Set("data[PARAMS][FILES][1][urlMachine]", "https://portal.bitrix24.com/disk/downloadFile/2/")
	v.Set("data[PARAMS][FILES][1][size]", "6789")

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}

	if len(evt.Params.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(evt.Params.Files))
	}
	if evt.Params.Files[0].Name != "cat.png" || evt.Params.Files[0].Type != "image" || evt.Params.Files[0].Size != 12345 {
		t.Errorf("file[0] mismatch: %+v", evt.Params.Files[0])
	}
	if evt.Params.Files[0].URL == "" {
		t.Errorf("file[0].URL missing (urlMachine should populate it)")
	}
	if evt.Params.Files[1].Name != "voice.ogg" || evt.Params.Files[1].Type != "audio" {
		t.Errorf("file[1] mismatch: %+v", evt.Params.Files[1])
	}
}

func TestParseEvent_SystemFlag(t *testing.T) {
	v := buildBitrixForm()
	v.Set("data[PARAMS][SYSTEM]", "Y")

	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if !evt.Params.SystemMessage {
		t.Errorf("SystemMessage expected true")
	}
}

func TestParseEvent_MissingEventType(t *testing.T) {
	v := url.Values{}
	v.Set("auth[domain]", "x.bitrix24.com")
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := ParseEvent(req)
	if err == nil {
		t.Fatal("expected error on missing event type")
	}
	if !strings.Contains(err.Error(), "missing event type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseEvent_EmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := ParseEvent(req)
	if err == nil {
		t.Fatal("expected error on empty body")
	}
}

func TestParseEvent_JSON(t *testing.T) {
	payload := map[string]any{
		"event": "ONIMBOTMESSAGEADD",
		"ts":    1713564321,
		"auth": map[string]any{
			"domain":            "portal.bitrix24.com",
			"application_token": "APPSECRET",
			"member_id":         "mem1",
			"expires_in":        3600,
		},
		"data": map[string]any{
			"BOT": map[string]any{
				"914": map[string]any{"BOT_ID": 914},
			},
			"PARAMS": map[string]any{
				"MESSAGE_ID":   42,
				"DIALOG_ID":    "chat1234",
				"FROM_USER_ID": 7,
				"MESSAGE":      "json hello",
				"MESSAGE_TYPE": "chat",
				"SYSTEM":       "N",
				"FILES": []map[string]any{
					{
						"id":         "f1",
						"name":       "doc.pdf",
						"type":       "file",
						"urlMachine": "https://portal.bitrix24.com/disk/downloadFile/5/",
						"size":       999,
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Type != "ONIMBOTMESSAGEADD" {
		t.Errorf("Type = %q", evt.Type)
	}
	if evt.Auth.Domain != "portal.bitrix24.com" || evt.Auth.AppToken != "APPSECRET" {
		t.Errorf("Auth = %+v", evt.Auth)
	}
	if evt.Auth.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d", evt.Auth.ExpiresIn)
	}
	if evt.Params.MessageID != "42" || evt.Params.BotID != 914 {
		t.Errorf("Params = %+v", evt.Params)
	}
	if evt.Params.FromUserID != "7" {
		t.Errorf("FromUserID = %q", evt.Params.FromUserID)
	}
	if evt.Params.Message != "json hello" {
		t.Errorf("Message = %q", evt.Params.Message)
	}
	if evt.Params.SystemMessage {
		t.Errorf("SystemMessage should be false when SYSTEM=N")
	}
	if len(evt.Params.Files) != 1 || evt.Params.Files[0].Size != 999 {
		t.Errorf("Files = %+v", evt.Params.Files)
	}
	if evt.Ts.Unix() != 1713564321 {
		t.Errorf("Ts = %v", evt.Ts)
	}
}

func TestParseEvent_JSON_MissingEvent(t *testing.T) {
	body := []byte(`{"auth":{"domain":"x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	_, err := ParseEvent(req)
	if err == nil {
		t.Fatal("expected error on missing event type (json)")
	}
}

func TestParseEvent_NilRequest(t *testing.T) {
	if _, err := ParseEvent(nil); err == nil {
		t.Fatal("expected error on nil request")
	}
}

// TestParseEvent_Form_ChatEntity verifies CHAT_ENTITY_TYPE + CHAT_ENTITY_ID
// surface on EventParams for both CRM-bound and Tasks-bound chats. These
// fields drive MCP "this deal/task" resolution downstream — without parsing
// them the agent has no deterministic way to know which entity the chat
// belongs to. Fixtures match real Bitrix24 webhooks captured against
// tamgiac.bitrix24.com (see plans/.../reports/event-payloads/05 + 07).
func TestParseEvent_Form_ChatEntity(t *testing.T) {
	cases := []struct {
		name           string
		entityType     string
		entityID       string
		messageType    string
	}{
		{name: "crm_deal", entityType: "CRM", entityID: "DEAL|2064", messageType: "C"},
		{name: "crm_lead", entityType: "CRM", entityID: "LEAD|7", messageType: "C"},
		{name: "tasks_task", entityType: "TASKS_TASK", entityID: "2704", messageType: "X"},
		{name: "plain_group_no_entity", entityType: "", entityID: "", messageType: "C"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := buildBitrixForm()
			v.Set("data[PARAMS][MESSAGE_TYPE]", tc.messageType)
			if tc.entityType != "" {
				v.Set("data[PARAMS][CHAT_ENTITY_TYPE]", tc.entityType)
			}
			if tc.entityID != "" {
				v.Set("data[PARAMS][CHAT_ENTITY_ID]", tc.entityID)
			}
			req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", strings.NewReader(v.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			evt, err := ParseEvent(req)
			if err != nil {
				t.Fatalf("ParseEvent: %v", err)
			}
			if evt.Params.ChatEntityType != tc.entityType {
				t.Errorf("ChatEntityType = %q; want %q", evt.Params.ChatEntityType, tc.entityType)
			}
			if evt.Params.ChatEntityID != tc.entityID {
				t.Errorf("ChatEntityID = %q; want %q", evt.Params.ChatEntityID, tc.entityID)
			}
		})
	}
}

// TestParseEvent_JSON_ChatEntity is the JSON-payload counterpart. Bitrix24
// rarely sends JSON in production but the parser accepts it, so we keep the
// two paths in lockstep.
func TestParseEvent_JSON_ChatEntity(t *testing.T) {
	payload := map[string]any{
		"event": "ONIMBOTMESSAGEADD",
		"auth":  map[string]any{"domain": "portal.bitrix24.com", "application_token": "X"},
		"data": map[string]any{
			"BOT": map[string]any{"914": map[string]any{"BOT_ID": 914}},
			"PARAMS": map[string]any{
				"MESSAGE_TYPE":     "X",
				"CHAT_ENTITY_TYPE": "TASKS_TASK",
				"CHAT_ENTITY_ID":   "2704",
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/bitrix24/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	evt, err := ParseEvent(req)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Params.ChatEntityType != "TASKS_TASK" {
		t.Errorf("ChatEntityType = %q", evt.Params.ChatEntityType)
	}
	if evt.Params.ChatEntityID != "2704" {
		t.Errorf("ChatEntityID = %q", evt.Params.ChatEntityID)
	}
}

func TestFormGet(t *testing.T) {
	v := url.Values{}
	v.Set("a[b][c]", "deep")
	v.Set("simple", "flat")

	if got := formGet(v, "simple"); got != "flat" {
		t.Errorf("flat: %q", got)
	}
	if got := formGet(v, "a", "b", "c"); got != "deep" {
		t.Errorf("nested: %q", got)
	}
	if got := formGet(v, "absent"); got != "" {
		t.Errorf("absent: %q (want empty)", got)
	}
}

func TestAsStringAndAsInt(t *testing.T) {
	cases := []struct {
		in    any
		asStr string
		asInt int
	}{
		{nil, "", 0},
		{"abc", "abc", 0},
		{"42", "42", 42},
		{42, "42", 42},
		{int64(99), "99", 99},
		{float64(12.0), "12", 12},
		{float64(3.14), "3.14", 3},
		{true, "Y", 0},
		{false, "N", 0},
		{json.Number("7"), "7", 7},
	}
	for _, c := range cases {
		if got := asString(c.in); got != c.asStr {
			t.Errorf("asString(%v) = %q, want %q", c.in, got, c.asStr)
		}
		if got := asInt(c.in); got != c.asInt {
			t.Errorf("asInt(%v) = %d, want %d", c.in, got, c.asInt)
		}
	}
}
