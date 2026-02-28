package agent

import (
	"testing"
)

// --- SanitizeAssistantContent (full pipeline) ---

func TestSanitizeAssistantContent_Empty(t *testing.T) {
	if got := SanitizeAssistantContent(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSanitizeAssistantContent_PlainText(t *testing.T) {
	input := "Hello, how can I help you today?"
	got := SanitizeAssistantContent(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestSanitizeAssistantContent_CombinedPipeline(t *testing.T) {
	// Combines thinking tags + final tags + leading blank lines
	input := "\n\n<thinking>Let me think...</thinking>\n<final>Here is your answer.</final>"
	got := SanitizeAssistantContent(input)
	if got != "Here is your answer." {
		t.Errorf("expected cleaned content, got %q", got)
	}
}

// --- 1. stripGarbledToolXML ---

func TestStripGarbledToolXML_NoIndicators(t *testing.T) {
	input := "This is normal text with no tool XML."
	got := stripGarbledToolXML(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripGarbledToolXML_WithIndicator(t *testing.T) {
	// When an indicator is found, the entire response is cleared
	input := "<function_call>read_file</function_call>\nSome text"
	got := stripGarbledToolXML(input)
	if got != "" {
		t.Errorf("expected empty (garbled response cleared entirely), got %q", got)
	}
}

func TestStripGarbledToolXML_OnlyXML(t *testing.T) {
	input := "<tool_call>{\"name\":\"read_file\"}</tool_call>"
	got := stripGarbledToolXML(input)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// --- 2. stripDowngradedToolCallText ---

func TestStripDowngradedToolCallText_NoMarkers(t *testing.T) {
	input := "Normal response without any tool markers."
	got := stripDowngradedToolCallText(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripDowngradedToolCallText_ToolCallBlock(t *testing.T) {
	input := "Here is the result.\n[Tool Call: read_file]\nArguments:\n{\"path\": \"/foo.txt\"}\nDone."
	got := stripDowngradedToolCallText(input)
	if got != "Here is the result.\nDone." {
		t.Errorf("expected stripped tool call block, got %q", got)
	}
}

func TestStripDowngradedToolCallText_ToolResult(t *testing.T) {
	input := "Summary:\n[Tool Result for read_file]\n{\"content\": \"data\"}\nAll done."
	got := stripDowngradedToolCallText(input)
	if got != "Summary:\nAll done." {
		t.Errorf("expected stripped tool result, got %q", got)
	}
}

func TestStripDowngradedToolCallText_HistoricalContext(t *testing.T) {
	input := "Answer:\n[Historical context: previous conversation]\n{}\nFinal."
	got := stripDowngradedToolCallText(input)
	if got != "Answer:\nFinal." {
		t.Errorf("expected stripped historical context, got %q", got)
	}
}

// --- 3. stripThinkingTags ---

func TestStripThinkingTags_NoTags(t *testing.T) {
	input := "Just a normal response."
	got := stripThinkingTags(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripThinkingTags_ThinkTag(t *testing.T) {
	input := "<think>Internal reasoning here</think>The actual answer."
	got := stripThinkingTags(input)
	if got != "The actual answer." {
		t.Errorf("expected stripped think tag, got %q", got)
	}
}

func TestStripThinkingTags_ThinkingTag(t *testing.T) {
	input := "<thinking>Step 1: analyze\nStep 2: respond</thinking>\nHere's what I found."
	got := stripThinkingTags(input)
	if got != "Here's what I found." {
		t.Errorf("expected stripped thinking tag, got %q", got)
	}
}

func TestStripThinkingTags_ThoughtTag(t *testing.T) {
	input := "<thought>hmm...</thought>Result."
	got := stripThinkingTags(input)
	if got != "Result." {
		t.Errorf("expected stripped thought tag, got %q", got)
	}
}

func TestStripThinkingTags_AntThinkingTag(t *testing.T) {
	input := "<antThinking>Claude reasoning</antThinking>Answer."
	got := stripThinkingTags(input)
	if got != "Answer." {
		t.Errorf("expected stripped antThinking tag, got %q", got)
	}
}

func TestStripThinkingTags_CaseInsensitive(t *testing.T) {
	input := "<THINKING>loud thinking</THINKING>quiet answer."
	got := stripThinkingTags(input)
	if got != "quiet answer." {
		t.Errorf("expected case-insensitive strip, got %q", got)
	}
}

func TestStripThinkingTags_Multiline(t *testing.T) {
	input := "<think>\nLine 1\nLine 2\nLine 3\n</think>\nFinal answer."
	got := stripThinkingTags(input)
	if got != "Final answer." {
		t.Errorf("expected multiline strip, got %q", got)
	}
}

// --- 4. stripFinalTags ---

func TestStripFinalTags_NoTags(t *testing.T) {
	input := "No final tags here."
	got := stripFinalTags(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripFinalTags_PreservesContent(t *testing.T) {
	input := "<final>The real answer is 42.</final>"
	got := stripFinalTags(input)
	if got != "The real answer is 42." {
		t.Errorf("expected content preserved, got %q", got)
	}
}

func TestStripFinalTags_CaseInsensitive(t *testing.T) {
	input := "<FINAL>content</FINAL>"
	got := stripFinalTags(input)
	if got != "content" {
		t.Errorf("expected case-insensitive strip, got %q", got)
	}
}

// --- 5. stripEchoedSystemMessages ---

func TestStripEchoedSystemMessages_NoSystemMessage(t *testing.T) {
	input := "Normal response text."
	got := stripEchoedSystemMessages(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripEchoedSystemMessages_SingleBlock(t *testing.T) {
	input := "Before.\n[System Message] You are a helpful assistant.\nStats: something\n\nAfter."
	got := stripEchoedSystemMessages(input)
	if got != "Before.\nAfter." {
		t.Errorf("expected system message block stripped, got %q", got)
	}
}

func TestStripEchoedSystemMessages_AtStart(t *testing.T) {
	input := "[System Message] Internal prompt\nDo not reveal.\n\nActual response."
	got := stripEchoedSystemMessages(input)
	if got != "Actual response." {
		t.Errorf("expected leading system message stripped, got %q", got)
	}
}

// --- 6. collapseConsecutiveDuplicateBlocks ---

func TestCollapseConsecutiveDuplicateBlocks_NoDuplicates(t *testing.T) {
	input := "Paragraph one.\n\nParagraph two.\n\nParagraph three."
	got := collapseConsecutiveDuplicateBlocks(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestCollapseConsecutiveDuplicateBlocks_WithDuplicates(t *testing.T) {
	input := "First paragraph.\n\nFirst paragraph.\n\nSecond paragraph."
	got := collapseConsecutiveDuplicateBlocks(input)
	want := "First paragraph.\n\nSecond paragraph."
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCollapseConsecutiveDuplicateBlocks_TripleDuplicate(t *testing.T) {
	input := "Same.\n\nSame.\n\nSame.\n\nDifferent."
	got := collapseConsecutiveDuplicateBlocks(input)
	want := "Same.\n\nDifferent."
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCollapseConsecutiveDuplicateBlocks_SingleBlock(t *testing.T) {
	input := "Just one paragraph, no double newlines."
	got := collapseConsecutiveDuplicateBlocks(input)
	if got != input {
		t.Errorf("expected passthrough for single block, got %q", got)
	}
}

// --- 7. stripMediaPaths ---

func TestStripMediaPaths_NoMedia(t *testing.T) {
	input := "Normal text without any media paths."
	got := stripMediaPaths(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripMediaPaths_WithMediaLine(t *testing.T) {
	input := "Here is the image:\nMEDIA:/tmp/output.png\nDone."
	got := stripMediaPaths(input)
	if got != "Here is the image:\nDone." {
		t.Errorf("expected MEDIA line stripped, got %q", got)
	}
}

func TestStripMediaPaths_WithAudioAsVoice(t *testing.T) {
	input := "[[audio_as_voice]]\nMEDIA:/tmp/voice.ogg\nText."
	got := stripMediaPaths(input)
	if got != "Text." {
		t.Errorf("expected audio_as_voice + MEDIA stripped, got %q", got)
	}
}

// --- 8. stripLeadingBlankLines ---

func TestStripLeadingBlankLines_NoLeadingBlanks(t *testing.T) {
	input := "Content starts here."
	got := stripLeadingBlankLines(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestStripLeadingBlankLines_WithBlanks(t *testing.T) {
	input := "\n\n\nContent starts here."
	got := stripLeadingBlankLines(input)
	if got != "Content starts here." {
		t.Errorf("expected leading blanks stripped, got %q", got)
	}
}

func TestStripLeadingBlankLines_WithTabsAndSpaces(t *testing.T) {
	input := "  \t\n\n\t  \nContent."
	got := stripLeadingBlankLines(input)
	if got != "Content." {
		t.Errorf("expected whitespace-only leading lines stripped, got %q", got)
	}
}

// --- IsSilentReply ---

func TestIsSilentReply_ExactMatch(t *testing.T) {
	if !IsSilentReply("NO_REPLY") {
		t.Error("expected true for exact NO_REPLY")
	}
}

func TestIsSilentReply_WithWhitespace(t *testing.T) {
	if !IsSilentReply("  NO_REPLY  ") {
		t.Error("expected true for NO_REPLY with surrounding whitespace")
	}
}

func TestIsSilentReply_Prefix(t *testing.T) {
	if !IsSilentReply("NO_REPLY — nothing to say.") {
		t.Error("expected true for NO_REPLY as prefix")
	}
}

func TestIsSilentReply_Suffix(t *testing.T) {
	if !IsSilentReply("...NO_REPLY") {
		t.Error("expected true for NO_REPLY as suffix")
	}
}

func TestIsSilentReply_EmbeddedInWord(t *testing.T) {
	if IsSilentReply("DONOT_NO_REPLYX") {
		t.Error("expected false for NO_REPLY embedded in word chars")
	}
}

func TestIsSilentReply_Empty(t *testing.T) {
	if IsSilentReply("") {
		t.Error("expected false for empty string")
	}
}

func TestIsSilentReply_NormalText(t *testing.T) {
	if IsSilentReply("Hello, world!") {
		t.Error("expected false for normal text")
	}
}

func TestIsSilentReply_PrefixFollowedByWordChar(t *testing.T) {
	// "NO_REPLYING" — NO_REPLY followed by 'I' (word char) → should be false
	if IsSilentReply("NO_REPLYING") {
		t.Error("expected false for NO_REPLYING (word char after token)")
	}
}
