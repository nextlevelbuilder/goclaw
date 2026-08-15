package agent

import (
	"strings"
	"testing"
)

func TestStripGarbledToolXML_FullToolCallBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "removes whole block, keeps surrounding prose",
			input: "PR #1217 looks good - approving.\n\n" +
				"<function_calls>\n<invoke name=\"send_message\">\n" +
				"<parameter name=\"channel\">argus</parameter>\n" +
				"<parameter name=\"text\">PR #1217 approved</parameter>\n" +
				"</invoke>\n</function_calls>",
			want: "PR #1217 looks good - approving.",
		},
		{
			name: "block-only response collapses to empty",
			input: "<function_calls><invoke name=\"send_message\">" +
				"<parameter name=\"text\">hi</parameter></invoke></function_calls>",
			want: "",
		},
		{
			name:  "no tool xml — unchanged",
			input: "Just a normal reply with no tool calls.",
			want:  "Just a normal reply with no tool calls.",
		},
		{
			name:  "partial stray tag still stripped (backward compat)",
			input: "Result here <tool_call>oops",
			want:  "Result here oops",
		},
		{
			name: "removes bare <invoke> block without function_calls wrapper",
			input: "Collecting PRs.\n\n" +
				"<invoke name=\"mcp__goclaw-bridge__exec\">\n" +
				"<parameter name=\"command\">gh pr view 1218</parameter>\n" +
				"</invoke>",
			want: "Collecting PRs.",
		},
		{
			name: "bare invoke-only response collapses to empty",
			input: "<invoke name=\"mcp__goclaw-bridge__message\">" +
				"<parameter name=\"text\">hi</parameter></invoke>",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripGarbledToolXML(tt.input)
			if got != tt.want {
				t.Errorf("stripGarbledToolXML() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Regression: the tag-only strip used to leave <parameter> argument values
// orphaned in the reply when a model emitted a tool call as text instead of
// invoking it natively (claude-cli thin-proxy). The whole block must be removed.
func TestSanitizeAssistantContent_NoToolCallArgumentLeak(t *testing.T) {
	input := "Done.\n<function_calls>\n<invoke name=\"send_message\">\n" +
		"<parameter name=\"channel\">argus</parameter>\n" +
		"<parameter name=\"text\">SECRET_ARG_VALUE</parameter>\n" +
		"</invoke>\n</function_calls>"

	got := SanitizeAssistantContent(input)

	if strings.Contains(got, "SECRET_ARG_VALUE") || strings.Contains(got, "argus") {
		t.Errorf("tool-call argument values leaked into reply: %q", got)
	}
	if got != "Done." {
		t.Errorf("SanitizeAssistantContent() = %q, want %q", got, "Done.")
	}
}

// Regression: claude-cli under a degraded session emits a tool call as a BARE
// <invoke> block with no <function_calls> wrapper. The whole block must be
// removed so the command argument does not leak into the user-facing reply.
func TestSanitizeAssistantContent_BareInvokeNoWrapper(t *testing.T) {
	input := "Format error, retrying.\n\n" +
		"<invoke name=\"mcp__goclaw-bridge__exec\">\n" +
		"<parameter name=\"command\">( cd ~/secret-path && gh pr view 1218 )</parameter>\n" +
		"</invoke>"

	got := SanitizeAssistantContent(input)

	if strings.Contains(got, "secret-path") || strings.Contains(got, "gh pr view") {
		t.Errorf("bare-invoke command argument leaked into reply: %q", got)
	}
	if got != "Format error, retrying." {
		t.Errorf("SanitizeAssistantContent() = %q, want %q", got, "Format error, retrying.")
	}
}
