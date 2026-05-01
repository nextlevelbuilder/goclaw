package agent

import (
	"strings"
	"testing"
)

// Test 8: ChatID present → prompt contains <current_reply_target> block.
func TestSystemPromptCurrentReplyTargetInjected(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "telegram"
	cfg.ChannelType = "telegram"
	cfg.ChatID = "123"
	cfg.PeerKind = "direct"

	prompt := BuildSystemPrompt(cfg)

	for _, want := range []string{
		"<current_reply_target>",
		"chat_id: 123",
		"kind: direct",
		"</current_reply_target>",
		"omit `target` to reply here",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// Test 8b: group peer → kind: group.
func TestSystemPromptCurrentReplyTargetGroup(t *testing.T) {
	cfg := fullTestConfig()
	cfg.Channel = "telegram"
	cfg.ChannelType = "telegram"
	cfg.ChatID = "-100G"
	cfg.PeerKind = "group"

	prompt := BuildSystemPrompt(cfg)

	if !strings.Contains(prompt, "chat_id: -100G") {
		t.Error("prompt missing group chat_id")
	}
	if !strings.Contains(prompt, "kind: group") {
		t.Error("prompt missing kind: group")
	}
}

// Test 9: ChatID empty → no <current_reply_target> block.
func TestSystemPromptCurrentReplyTargetOmittedWhenNoChat(t *testing.T) {
	cfg := fullTestConfig()
	cfg.ChatID = ""
	prompt := BuildSystemPrompt(cfg)
	if strings.Contains(prompt, "<current_reply_target>") {
		t.Error("prompt should NOT include <current_reply_target> when ChatID is empty")
	}
}
