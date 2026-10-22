package agent

import (
	"strings"
	"testing"
)

func TestSystemPromptFormattingHint_MattermostAttachments(t *testing.T) {
	cfg := fullTestConfig()
	cfg.ChannelType = "mattermost"

	prompt := BuildSystemPrompt(cfg)

	assertPromptContains(t, prompt, []string{
		"This channel (Mattermost) renders standard Markdown",
		"fenced code block with the language `attachment`",
	})
}

func TestSystemPromptFormattingHint_AbsentForOtherChannels(t *testing.T) {
	cfg := fullTestConfig()
	cfg.ChannelType = "telegram"

	if prompt := BuildSystemPrompt(cfg); strings.Contains(prompt, "## Output Formatting") {
		t.Fatal("telegram must not get a formatting hint")
	}
}
