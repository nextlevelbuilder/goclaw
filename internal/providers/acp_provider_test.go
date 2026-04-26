package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractACPContent_NormalTurn verifies that isNew=false sends only the
// current user message without system prompt prepend.
func TestExtractACPContent_NormalTurn(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are Ender."},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "current question"},
		},
	}
	blocks := extractACPContent(req, false)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if blocks[0].Text != "current question" {
		t.Errorf("want only current user message, got: %q", blocks[0].Text)
	}
	if strings.Contains(blocks[0].Text, "You are Ender") {
		t.Error("system prompt must not appear in normal-turn content")
	}
}

// TestExtractACPContent_NewSession_WithHistory verifies that isNew=true serialises
// the full conversation (summary + history + current) excluding the system prompt.
func TestExtractACPContent_NewSession_WithHistory(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are Ender."},
			{Role: "user", Content: "[Previous conversation summary]\nDiscussed KIS API setup."},
			{Role: "assistant", Content: "I understand the context from our previous conversation. How can I help you?"},
			{Role: "user", Content: "turn1 user"},
			{Role: "assistant", Content: "turn1 asst"},
			{Role: "user", Content: "current question"},
		},
	}
	blocks := extractACPContent(req, true)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	text := blocks[0].Text

	// system must be excluded
	if strings.Contains(text, "You are Ender") {
		t.Error("system prompt must not appear in new-session transcript")
	}
	// summary must be present
	if !strings.Contains(text, "Previous conversation summary") {
		t.Error("episodic summary must be included in new-session transcript")
	}
	// history must be present
	if !strings.Contains(text, "turn1 user") || !strings.Contains(text, "turn1 asst") {
		t.Error("conversation history must be included in new-session transcript")
	}
	// current message must be present
	if !strings.Contains(text, "current question") {
		t.Error("current user message must be included in new-session transcript")
	}
	// role markers
	if !strings.Contains(text, "[User]") || !strings.Contains(text, "[Assistant]") {
		t.Error("role markers [User]/[Assistant] must be present")
	}
}

// TestExtractACPContent_NewSession_FirstEver verifies isNew=true with no prior
// history (very first message) behaves correctly and still includes current message.
func TestExtractACPContent_NewSession_FirstEver(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are Ender."},
			{Role: "user", Content: "first ever message"},
		},
	}
	blocks := extractACPContent(req, true)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Text, "first ever message") {
		t.Errorf("current message must be present, got: %q", blocks[0].Text)
	}
	if strings.Contains(blocks[0].Text, "You are Ender") {
		t.Error("system prompt must not appear even on first-ever message")
	}
}

// TestExtractACPContent_NoUserMessage verifies that an empty request returns nil.
func TestExtractACPContent_NoUserMessage(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are Ender."},
		},
	}
	if got := extractACPContent(req, false); got != nil {
		t.Errorf("want nil for missing user message, got %v", got)
	}
	if got := extractACPContent(req, true); got != nil {
		t.Errorf("want nil for missing user message (isNew), got %v", got)
	}
}

// TestWriteGeminiMD_WritesFile verifies the file is written and true is returned.
func TestWriteGeminiMD_WritesFile(t *testing.T) {
	dir := t.TempDir()
	p := &ACPProvider{}

	changed := p.writeGeminiMD(dir, "system prompt content")
	if !changed {
		t.Fatal("want changed=true for new file")
	}
	data, err := os.ReadFile(filepath.Join(dir, "GEMINI.md"))
	if err != nil {
		t.Fatalf("GEMINI.md not created: %v", err)
	}
	if string(data) != "system prompt content" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

// TestWriteGeminiMD_NoopIfUnchanged verifies no write and false return when unchanged.
func TestWriteGeminiMD_NoopIfUnchanged(t *testing.T) {
	dir := t.TempDir()
	p := &ACPProvider{}

	p.writeGeminiMD(dir, "same content")
	path := filepath.Join(dir, "GEMINI.md")
	info1, _ := os.Stat(path)

	changed := p.writeGeminiMD(dir, "same content")
	if changed {
		t.Fatal("want changed=false when content is identical")
	}
	info2, _ := os.Stat(path)
	if info1.ModTime() != info2.ModTime() {
		t.Error("file must not be rewritten when content is unchanged")
	}
}

// TestWriteGeminiMD_UpdatesOnChange verifies file is rewritten and true returned when content changes.
func TestWriteGeminiMD_UpdatesOnChange(t *testing.T) {
	dir := t.TempDir()
	p := &ACPProvider{}

	p.writeGeminiMD(dir, "old system prompt")
	changed := p.writeGeminiMD(dir, "new system prompt")
	if !changed {
		t.Fatal("want changed=true when content differs")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "GEMINI.md"))
	if string(data) != "new system prompt" {
		t.Errorf("expected updated content, got: %q", string(data))
	}
}
