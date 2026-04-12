package feishu

import (
	"testing"
)

// --- parseMessageContent ---

func TestParseMessageContent_Text(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
	}{
		{"normal text", `{"text":"hello world"}`, "hello world"},
		{"empty raw", "", ""},
		{"invalid json falls back", "not-json", "not-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMessageContent(tc.raw, "text")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseMessageContent_Image(t *testing.T) {
	got := parseMessageContent(`{"image_key":"img_123"}`, "image")
	if got != "[image]" {
		t.Errorf("got %q, want \"[image]\"", got)
	}
}

func TestParseMessageContent_File(t *testing.T) {
	got := parseMessageContent(`{"file_key":"f1","file_name":"report.pdf"}`, "file")
	if got != "[file: report.pdf]" {
		t.Errorf("got %q, want \"[file: report.pdf]\"", got)
	}
}

func TestParseMessageContent_File_NoName(t *testing.T) {
	got := parseMessageContent("not-json", "file")
	if got != "[file]" {
		t.Errorf("got %q, want \"[file]\"", got)
	}
}

func TestParseMessageContent_UnknownType(t *testing.T) {
	got := parseMessageContent(`{}`, "sticker")
	if got != "[sticker message]" {
		t.Errorf("got %q, want \"[sticker message]\"", got)
	}
}

// --- parsePostContent ---

func TestParsePostContent_FlatFormat(t *testing.T) {
	// Format 2: flat, "content" at top level
	raw := `{"content":[[{"tag":"text","text":"hello"},{"tag":"text","text":" world"}]]}`
	got := parsePostContent(raw)
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestParsePostContent_LanguageWrapped_ZhCN(t *testing.T) {
	raw := `{"zh_cn":{"title":"Title","content":[[{"tag":"text","text":"你好"}]]}}`
	got := parsePostContent(raw)
	if got != "你好" {
		t.Errorf("got %q, want %q", got, "你好")
	}
}

func TestParsePostContent_LanguageWrapped_EnUS(t *testing.T) {
	raw := `{"en_us":{"content":[[{"tag":"md","text":"**bold**"}]]}}`
	got := parsePostContent(raw)
	if got != "**bold**" {
		t.Errorf("got %q, want %q", got, "**bold**")
	}
}

func TestParsePostContent_MultiParagraph(t *testing.T) {
	raw := `{"content":[[{"tag":"text","text":"line1"}],[{"tag":"text","text":"line2"}]]}`
	got := parsePostContent(raw)
	if got != "line1\nline2" {
		t.Errorf("got %q, want %q", got, "line1\nline2")
	}
}

func TestParsePostContent_AtTag(t *testing.T) {
	raw := `{"content":[[{"tag":"at","user_name":"Alice"},{"tag":"text","text":" hi"}]]}`
	got := parsePostContent(raw)
	if got != "@Alice hi" {
		t.Errorf("got %q, want %q", got, "@Alice hi")
	}
}

func TestParsePostContent_LinkTag(t *testing.T) {
	raw := `{"content":[[{"tag":"a","href":"https://example.com","text":"click"}]]}`
	got := parsePostContent(raw)
	if got != "[click](https://example.com)" {
		t.Errorf("got %q, want %q", got, "[click](https://example.com)")
	}
}

func TestParsePostContent_LinkTag_NoText(t *testing.T) {
	raw := `{"content":[[{"tag":"a","href":"https://example.com"}]]}`
	got := parsePostContent(raw)
	if got != "https://example.com" {
		t.Errorf("got %q, want %q", got, "https://example.com")
	}
}

func TestParsePostContent_ImageTag(t *testing.T) {
	raw := `{"content":[[{"tag":"img","image_key":"img_abc"}]]}`
	got := parsePostContent(raw)
	if got != "[image]" {
		t.Errorf("got %q, want %q", got, "[image]")
	}
}

func TestParsePostContent_InvalidJSON(t *testing.T) {
	// Falls back to raw content
	raw := "not-json"
	got := parsePostContent(raw)
	if got != raw {
		t.Errorf("got %q, want %q", got, raw)
	}
}

// --- resolvePostElements ---

func TestResolvePostElements_FirstMapValueFallback(t *testing.T) {
	// No zh_cn/en_us key — uses first map value as language wrapper
	raw := `{"custom_lang":{"content":[[{"tag":"text","text":"fallback"}]]}}`
	elems := resolvePostElements(raw)
	if len(elems) == 0 {
		t.Fatal("expected elements, got none")
	}
}

func TestResolvePostElements_NoContent(t *testing.T) {
	raw := `{"zh_cn":{"title":"only title"}}`
	elems := resolvePostElements(raw)
	if elems != nil {
		t.Errorf("expected nil, got %v", elems)
	}
}

// --- extractPostImageKeys ---

func TestExtractPostImageKeys_Basic(t *testing.T) {
	raw := `{"content":[[{"tag":"img","image_key":"img_001"},{"tag":"text","text":"caption"}],[{"tag":"img","image_key":"img_002"}]]}`
	keys := extractPostImageKeys(raw)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "img_001" || keys[1] != "img_002" {
		t.Errorf("keys: got %v", keys)
	}
}

func TestExtractPostImageKeys_Dedup(t *testing.T) {
	raw := `{"content":[[{"tag":"img","image_key":"img_dup"}],[{"tag":"img","image_key":"img_dup"}]]}`
	keys := extractPostImageKeys(raw)
	if len(keys) != 1 {
		t.Errorf("expected 1 deduped key, got %d: %v", len(keys), keys)
	}
}

func TestExtractPostImageKeys_NoImages(t *testing.T) {
	raw := `{"content":[[{"tag":"text","text":"no images"}]]}`
	keys := extractPostImageKeys(raw)
	if keys != nil {
		t.Errorf("expected nil, got %v", keys)
	}
}

// --- resolveMentions ---

func TestResolveMentions_BotMentionStripped(t *testing.T) {
	text := "@_user_1 hello"
	mentions := []mentionInfo{{Key: "@_user_1", OpenID: "ou_bot123", Name: "Bot"}}
	got := resolveMentions(text, mentions, "ou_bot123")
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestResolveMentions_UserMentionReplaced(t *testing.T) {
	text := "hey @_user_1 how are you"
	mentions := []mentionInfo{{Key: "@_user_1", OpenID: "ou_user999", Name: "Alice"}}
	got := resolveMentions(text, mentions, "ou_bot123")
	if got != "hey @Alice how are you" {
		t.Errorf("got %q, want %q", got, "hey @Alice how are you")
	}
}

func TestResolveMentions_EmptyKey_Skipped(t *testing.T) {
	text := "hello"
	mentions := []mentionInfo{{Key: "", OpenID: "ou_abc", Name: "Bob"}}
	got := resolveMentions(text, mentions, "")
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestResolveMentions_NoBotID_AllMentionsReplacedWithName(t *testing.T) {
	text := "@_user_1 and @_user_2"
	mentions := []mentionInfo{
		{Key: "@_user_1", OpenID: "ou_A", Name: "Alice"},
		{Key: "@_user_2", OpenID: "ou_B", Name: "Bob"},
	}
	got := resolveMentions(text, mentions, "")
	// botOpenID is empty: all mentions treated as bot → stripped
	// (because the condition is: if botOpenID != "" && openID == botOpenID → strip, else → replace with name)
	// When botOpenID == "", neither branch strips unless openID matches empty string.
	// Both get replaced with @Name.
	if got != "@Alice and @Bob" {
		t.Errorf("got %q, want %q", got, "@Alice and @Bob")
	}
}

// --- parseMessageEvent (via Channel) ---

func TestParseMessageEvent_TextMessage(t *testing.T) {
	ch := &Channel{}
	ev := &MessageEvent{}
	ev.Event.Message.MessageID = "om_abc"
	ev.Event.Message.ChatID = "oc_chat_1"
	ev.Event.Message.ChatType = "p2p"
	ev.Event.Message.MessageType = "text"
	ev.Event.Message.Content = `{"text":"hello"}`
	ev.Event.Sender.SenderID.OpenID = "ou_sender1"

	mc := ch.parseMessageEvent(ev)
	if mc == nil {
		t.Fatal("got nil messageContext")
	}
	if mc.Content != "hello" {
		t.Errorf("content: got %q, want %q", mc.Content, "hello")
	}
	if mc.ChatID != "oc_chat_1" {
		t.Errorf("ChatID: got %q", mc.ChatID)
	}
	if mc.SenderID != "ou_sender1" {
		t.Errorf("SenderID: got %q", mc.SenderID)
	}
	if mc.MentionedBot {
		t.Error("MentionedBot should be false for text with no mentions")
	}
}

func TestParseMessageEvent_BotMentioned(t *testing.T) {
	ch := &Channel{botOpenID: "ou_bot_xyz"}
	ev := &MessageEvent{}
	ev.Event.Message.MessageType = "text"
	ev.Event.Message.Content = `{"text":"@_user_1 please help"}`
	ev.Event.Message.Mentions = []EventMention{
		{Key: "@_user_1", ID: struct {
			OpenID  string `json:"open_id"`
			UserID  string `json:"user_id"`
			UnionID string `json:"union_id"`
		}{OpenID: "ou_bot_xyz"}, Name: "MyBot"},
	}

	mc := ch.parseMessageEvent(ev)
	if !mc.MentionedBot {
		t.Error("MentionedBot should be true when bot's open_id is mentioned")
	}
	// Bot mention key should be stripped from content
	if mc.Content == "@_user_1 please help" {
		t.Error("bot mention key should be stripped from content")
	}
}

func TestParseMessageEvent_NoSender(t *testing.T) {
	ch := &Channel{}
	ev := &MessageEvent{}
	ev.Event.Message.MessageType = "text"
	ev.Event.Message.Content = `{"text":"hi"}`
	// Sender fields are zero-value

	mc := ch.parseMessageEvent(ev)
	if mc.SenderID != "" {
		t.Errorf("SenderID: got %q, want empty", mc.SenderID)
	}
}
