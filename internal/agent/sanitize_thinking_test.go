package agent

import "testing"

func TestStripThinkingTags_RedactedThinking(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{
			name:  "strips redacted_thinking tags",
			input: "Hello <redacted_thinking>secret reasoning</redacted_thinking> world",
			want:  "Hello  world",
		},
		{
			name:  "strips redacted_thinking with attributes",
			input: `Before <redacted_thinking data="abc">hidden</redacted_thinking> after`,
			want:  "Before  after",
		},
		{
			name:  "strips redacted_thinking with whitespace in closing tag",
			input: "A <redacted_thinking>x</redacted_thinking  > B",
			want:  "A  B",
		},
		{
			name:  "strips redacted_thinking multiline content",
			input: "Start\n<redacted_thinking>\nline1\nline2\n</redacted_thinking>\nEnd",
			want:  "Start\n\nEnd",
		},
		{
			name:  "content with only redacted_thinking (no other thinking tags)",
			input: "<redacted_thinking>all hidden</redacted_thinking>",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkingTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkingTags() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripThinkingTags_ExistingPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips think tags",
			input: "Hello <think>reasoning</think> world",
			want:  "Hello  world",
		},
		{
			name:  "strips thinking tags",
			input: "A <thinking>step by step</thinking> B",
			want:  "A  B",
		},
		{
			name:  "strips thought tags",
			input: "A <thought>hmm</thought> B",
			want:  "A  B",
		},
		{
			name:  "strips antThinking tags",
			input: "A <antThinking>internal</antThinking> B",
			want:  "A  B",
		},
		{
			name:  "strips think with attributes",
			input: `A <think lang="en">stuff</think> B`,
			want:  "A  B",
		},
		{
			name:  "no thinking tags — returns unchanged",
			input: "Just normal content here",
			want:  "Just normal content here",
		},
		{
			name:  "mixed redacted_thinking and think",
			input: "<redacted_thinking>a</redacted_thinking> middle <think>b</think>",
			want:  "middle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkingTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkingTags() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeAssistantContent_RedactedThinking(t *testing.T) {
	input := "Hello <redacted_thinking>secret</redacted_thinking> world"
	got := SanitizeAssistantContent(input)
	if got != "Hello  world" {
		t.Errorf("SanitizeAssistantContent() = %q, want %q", got, "Hello  world")
	}
}
