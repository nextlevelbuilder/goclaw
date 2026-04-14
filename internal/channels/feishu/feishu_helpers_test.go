package feishu

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// --- resolveDomain ---

func TestResolveDomain(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"feishu", "https://open.feishu.cn"},
		{"lark", "https://open.larksuite.com"},
		{"", "https://open.larksuite.com"},
		{"https://custom.example.com", "https://custom.example.com"},
		{"http://custom.example.com", "http://custom.example.com"},
		{"custom.example.com", "https://custom.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := resolveDomain(tc.input)
			if got != tc.want {
				t.Errorf("resolveDomain(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- resolveReceiveIDType ---

func TestResolveReceiveIDType(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"oc_chat123", "chat_id"},
		{"ou_user456", "open_id"},
		{"on_union789", "union_id"},
		{"group:oc_chat123", "chat_id"}, // unknown prefix falls back to chat_id
		{"", "chat_id"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got := resolveReceiveIDType(tc.id)
			if got != tc.want {
				t.Errorf("resolveReceiveIDType(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

// --- hasMentions ---

func TestHasMentions(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"hello @ou_abc123", true},
		{"no mentions here", false},
		{"@ou_xyz at start", true},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := hasMentions(tc.text)
			if got != tc.want {
				t.Errorf("hasMentions(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// --- convertMentionsForCard ---

func TestConvertMentionsForCard(t *testing.T) {
	input := "Hello @ou_abc123 and @ou_def456"
	got := convertMentionsForCard(input)
	if !strings.Contains(got, "<at id=ou_abc123></at>") {
		t.Errorf("missing first mention in: %q", got)
	}
	if !strings.Contains(got, "<at id=ou_def456></at>") {
		t.Errorf("missing second mention in: %q", got)
	}
}

func TestConvertMentionsForCard_NoMentions(t *testing.T) {
	input := "plain text"
	got := convertMentionsForCard(input)
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

// --- buildPostContent ---

func TestBuildPostContent_NoMentions(t *testing.T) {
	content := buildPostContent("simple text")
	if !strings.Contains(content, "simple text") {
		t.Errorf("content missing text: %q", content)
	}
	if !strings.Contains(content, "zh_cn") {
		t.Errorf("content missing zh_cn wrapper: %q", content)
	}
	if !strings.Contains(content, `"tag":"md"`) {
		t.Errorf("content missing md tag: %q", content)
	}
}

func TestBuildPostContent_WithMention(t *testing.T) {
	content := buildPostContent("Hey @ou_abc123 check this")
	if !strings.Contains(content, `"tag":"at"`) {
		t.Errorf("content missing at tag: %q", content)
	}
	if !strings.Contains(content, "ou_abc123") {
		t.Errorf("content missing user_id: %q", content)
	}
}

func TestBuildPostContent_MentionAtStart(t *testing.T) {
	content := buildPostContent("@ou_abc123 please review")
	if !strings.Contains(content, `"tag":"at"`) {
		t.Errorf("content missing at tag: %q", content)
	}
	if !strings.Contains(content, "please review") {
		t.Errorf("content missing trailing text: %q", content)
	}
}

func TestBuildPostContent_MentionAtEnd(t *testing.T) {
	content := buildPostContent("please review @ou_xyz999")
	if !strings.Contains(content, "please review") {
		t.Errorf("content missing leading text: %q", content)
	}
	if !strings.Contains(content, "ou_xyz999") {
		t.Errorf("content missing user_id: %q", content)
	}
}

// --- buildMarkdownCard ---

func TestBuildMarkdownCard_Structure(t *testing.T) {
	card := buildMarkdownCard("**hello**")
	if card["schema"] != "2.0" {
		t.Errorf("schema: got %v, want 2.0", card["schema"])
	}
	if card["config"] == nil {
		t.Error("config must not be nil")
	}
	if card["body"] == nil {
		t.Error("body must not be nil")
	}
}

// --- shouldUseCard (package-level func: text → bool) ---

func TestShouldUseCard(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"```go\nfmt.Println()\n```", true},
		{"| col1 | col2 |\n| --- | --- |", true},
		{"|---|---|\n|a|b|", true},
		{"plain text no code", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.text[:min(20, len(tc.text))], func(t *testing.T) {
			got := shouldUseCard(tc.text)
			if got != tc.want {
				t.Errorf("shouldUseCard(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// min is a local helper for Go < 1.21 compatibility in test labels.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- isDuplicate ---

func TestIsDuplicate_FirstTimeFalse(t *testing.T) {
	ch := &Channel{}
	if ch.isDuplicate("msg_001") {
		t.Error("first call must return false")
	}
}

func TestIsDuplicate_SecondTimeTrue(t *testing.T) {
	ch := &Channel{}
	ch.isDuplicate("msg_002") // first call stores it
	if !ch.isDuplicate("msg_002") {
		t.Error("second call must return true")
	}
}

func TestIsDuplicate_DifferentIDs(t *testing.T) {
	ch := &Channel{}
	ch.isDuplicate("msg_A")
	if ch.isDuplicate("msg_B") {
		t.Error("different IDs must not be duplicates")
	}
}

// --- BlockReplyEnabled (returns *bool, cfg-level override) ---

func TestBlockReplyEnabled_Default(t *testing.T) {
	ch := &Channel{}
	// nil pointer for BlockReply → returns nil
	if result := ch.BlockReplyEnabled(); result != nil {
		t.Errorf("BlockReplyEnabled should return nil by default, got %v", *result)
	}
}

func TestBlockReplyEnabled_True(t *testing.T) {
	b := true
	ch := &Channel{cfg: config.FeishuConfig{
		AppID: "test", AppSecret: "test",
		BlockReply: &b,
	}}
	result := ch.BlockReplyEnabled()
	if result == nil || !*result {
		t.Error("BlockReplyEnabled should return pointer to true")
	}
}

func TestBlockReplyEnabled_False(t *testing.T) {
	b := false
	ch := &Channel{cfg: config.FeishuConfig{
		AppID: "test", AppSecret: "test",
		BlockReply: &b,
	}}
	result := ch.BlockReplyEnabled()
	if result == nil || *result {
		t.Error("BlockReplyEnabled should return pointer to false")
	}
}
