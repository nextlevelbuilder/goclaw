package whatsapp

import (
	"strings"
	"testing"
)

// --- chunkText ---

func TestChunkText_FitsInOne(t *testing.T) {
	got := chunkText("hello world", 100)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("chunkText short text = %v, want [hello world]", got)
	}
}

func TestChunkText_ExactLength(t *testing.T) {
	text := "hello"
	got := chunkText(text, 5)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("chunkText exact length = %v, want [hello]", got)
	}
}

func TestChunkText_SplitAtParagraph(t *testing.T) {
	text := "para one\n\npara two"
	got := chunkText(text, 12)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "para one") {
		t.Errorf("first chunk should contain 'para one', got %q", got[0])
	}
	if !strings.Contains(got[1], "para two") {
		t.Errorf("second chunk should contain 'para two', got %q", got[1])
	}
}

func TestChunkText_SplitAtNewline(t *testing.T) {
	text := "line one\nline two\nline three"
	got := chunkText(text, 14)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(got), got)
	}
	// All content must be preserved.
	joined := strings.Join(got, "\n")
	for _, word := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q lost after chunking, got: %v", word, got)
		}
	}
}

func TestChunkText_SplitAtSpace(t *testing.T) {
	text := "word1 word2 word3 word4"
	got := chunkText(text, 12)
	// No chunk should exceed maxLen.
	for _, chunk := range got {
		if len(chunk) > 12 {
			t.Errorf("chunk %q exceeds maxLen 12", chunk)
		}
	}
	// All words must be present.
	joined := strings.Join(got, " ")
	for _, word := range []string{"word1", "word2", "word3", "word4"} {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q lost after chunking, got: %v", word, got)
		}
	}
}

func TestChunkText_HardCut(t *testing.T) {
	// No whitespace — must hard-cut.
	text := "abcdefghij"
	got := chunkText(text, 5)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d: %v", len(got), got)
	}
	for _, chunk := range got {
		if len(chunk) > 5 {
			t.Errorf("chunk %q exceeds maxLen 5", chunk)
		}
	}
	// All characters must survive.
	joined := strings.Join(got, "")
	if joined != text {
		t.Errorf("hard-cut lost content: joined=%q, want=%q", joined, text)
	}
}

func TestChunkText_EmptyString(t *testing.T) {
	got := chunkText("", 100)
	// Empty input: function returns nil (no chunks) or empty slice — acceptable.
	// The caller (Send) handles this gracefully.
	for _, chunk := range got {
		if chunk != "" {
			t.Errorf("unexpected non-empty chunk for empty input: %q", chunk)
		}
	}
}

func TestChunkText_ChunksDoNotExceedMaxLen(t *testing.T) {
	// Property test: no chunk should ever exceed maxLen.
	inputs := []string{
		"The quick brown fox jumps over the lazy dog",
		"a\nb\nc\nd\ne\nf\ng",
		"para1\n\npara2\n\npara3\n\npara4",
		strings.Repeat("x", 200),
	}
	const maxLen = 20
	for _, input := range inputs {
		got := chunkText(input, maxLen)
		for _, chunk := range got {
			if len(chunk) > maxLen {
				t.Errorf("input %q: chunk %q exceeds maxLen %d", input[:min(20, len(input))], chunk, maxLen)
			}
		}
	}
}

// --- markdownToWhatsApp: additional edge cases ---

func TestMarkdownToWhatsApp_Empty(t *testing.T) {
	got := markdownToWhatsApp("")
	if got != "" {
		t.Errorf("markdownToWhatsApp(\"\") = %q, want empty", got)
	}
}

func TestMarkdownToWhatsApp_OnlyWhitespace(t *testing.T) {
	got := markdownToWhatsApp("   \n\n   ")
	// TrimSpace at end should produce empty.
	if got != "" {
		t.Errorf("markdownToWhatsApp(whitespace) = %q, want empty", got)
	}
}

func TestMarkdownToWhatsApp_CollapseBlankLines(t *testing.T) {
	got := markdownToWhatsApp("a\n\n\n\n\nb")
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank lines not collapsed, got: %q", got)
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("content lost after collapsing blank lines, got: %q", got)
	}
}

func TestMarkdownToWhatsApp_MixedFormatting(t *testing.T) {
	input := "# Title\n\n**bold** and ~~strike~~ and `code` and [link](https://x.com)"
	got := markdownToWhatsApp(input)

	// Header should become bold.
	if !strings.Contains(got, "*Title*") {
		t.Errorf("header should become *bold*, got: %q", got)
	}
	// **bold** → *bold*
	if !strings.Contains(got, "*bold*") {
		t.Errorf("**bold** should become *bold*, got: %q", got)
	}
	// ~~strike~~ → ~strike~
	if !strings.Contains(got, "~strike~") {
		t.Errorf("~~strike~~ should become ~strike~, got: %q", got)
	}
	// `code` → ```code```
	if !strings.Contains(got, "```code```") {
		t.Errorf("`code` should become ```code```, got: %q", got)
	}
	// Link → text url
	if !strings.Contains(got, "link https://x.com") {
		t.Errorf("[link](url) should become 'link url', got: %q", got)
	}
}

func TestMarkdownToWhatsApp_CodeBlockPreservedInternals(t *testing.T) {
	// Internal formatting inside code blocks must not be mangled by outer regex passes.
	// waExtractCodeBlocks pulls out the block before bold/italic regexes run, then restores it verbatim.
	input := "```\n**not bold** and _not italic_\n```"
	got := markdownToWhatsApp(input)

	// The raw markdown inside the block must be restored as-is (not converted to *not bold*).
	if !strings.Contains(got, "**not bold**") {
		t.Errorf("code block internal **...** should NOT be converted to *..*, got: %q", got)
	}
	// Code block delimiters must be present.
	if !strings.Contains(got, "```") {
		t.Errorf("code block fences should be preserved, got: %q", got)
	}
}

func TestMarkdownToWhatsApp_OrderedListPassthrough(t *testing.T) {
	// Ordered lists (1. item) are not converted — WhatsApp has no ordered list syntax.
	input := "1. first\n2. second\n3. third"
	got := markdownToWhatsApp(input)
	// Numbers should survive (not be mangled).
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("ordered list content lost, got: %q", got)
	}
}

func TestMarkdownToWhatsApp_HTMLStrongTag(t *testing.T) {
	got := markdownToWhatsApp("<strong>bold</strong>")
	if !strings.Contains(got, "*bold*") {
		t.Errorf("<strong>bold</strong> should become *bold*, got: %q", got)
	}
}

func TestMarkdownToWhatsApp_HTMLParagraph(t *testing.T) {
	got := markdownToWhatsApp("<p>paragraph</p>")
	if !strings.Contains(got, "paragraph") {
		t.Errorf("paragraph content should survive <p> tag conversion, got: %q", got)
	}
}

func TestMarkdownToWhatsApp_MultipleCodeBlocks(t *testing.T) {
	input := "```go\nfmt.Println(\"hi\")\n```\n\nText\n\n```py\nprint('hello')\n```"
	got := markdownToWhatsApp(input)

	// Both code blocks must survive.
	if !strings.Contains(got, "fmt.Println") {
		t.Errorf("first code block content lost, got: %q", got)
	}
	if !strings.Contains(got, "print('hello')") {
		t.Errorf("second code block content lost, got: %q", got)
	}
}

// --- waExtractCodeBlocks ---

func TestWAExtractCodeBlocks_NoBlocks(t *testing.T) {
	result := waExtractCodeBlocks("plain text")
	if len(result.codes) != 0 {
		t.Errorf("expected 0 code blocks, got %d", len(result.codes))
	}
	if result.text != "plain text" {
		t.Errorf("text should be unchanged, got %q", result.text)
	}
}

func TestWAExtractCodeBlocks_SingleBlock(t *testing.T) {
	result := waExtractCodeBlocks("```\ncode here\n```")
	if len(result.codes) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(result.codes))
	}
	if !strings.Contains(result.codes[0], "code here") {
		t.Errorf("code block content not captured: %q", result.codes[0])
	}
	if !strings.Contains(result.text, "\x00CB0\x00") {
		t.Errorf("placeholder not inserted in text: %q", result.text)
	}
}

func TestWAExtractCodeBlocks_MultipleBlocks(t *testing.T) {
	input := "```\nblock1\n```\nmiddle\n```\nblock2\n```"
	result := waExtractCodeBlocks(input)
	if len(result.codes) != 2 {
		t.Fatalf("expected 2 code blocks, got %d: %v", len(result.codes), result.codes)
	}
}

func TestWAExtractCodeBlocks_WithLanguageAnnotation(t *testing.T) {
	result := waExtractCodeBlocks("```python\nprint('hi')\n```")
	if len(result.codes) != 1 {
		t.Fatalf("expected 1 code block with language, got %d", len(result.codes))
	}
	if !strings.Contains(result.codes[0], "print('hi')") {
		t.Errorf("code block content missing: %q", result.codes[0])
	}
}

// --- htmlTagToWaMd ---

func TestHTMLTagToWaMd_Bold(t *testing.T) {
	got := htmlTagToWaMd("<b>text</b>")
	if !strings.Contains(got, "**text**") {
		t.Errorf("htmlTagToWaMd(<b>) = %q, want **text**", got)
	}
}

func TestHTMLTagToWaMd_Strong(t *testing.T) {
	got := htmlTagToWaMd("<strong>text</strong>")
	if !strings.Contains(got, "**text**") {
		t.Errorf("htmlTagToWaMd(<strong>) = %q, want **text**", got)
	}
}

func TestHTMLTagToWaMd_Strike(t *testing.T) {
	got := htmlTagToWaMd("<s>text</s>")
	if !strings.Contains(got, "~~text~~") {
		t.Errorf("htmlTagToWaMd(<s>) = %q, want ~~text~~", got)
	}
}

func TestHTMLTagToWaMd_Code(t *testing.T) {
	got := htmlTagToWaMd("<code>var</code>")
	if !strings.Contains(got, "`var`") {
		t.Errorf("htmlTagToWaMd(<code>) = %q, want `var`", got)
	}
}

func TestHTMLTagToWaMd_Link(t *testing.T) {
	got := htmlTagToWaMd(`<a href="https://example.com">click</a>`)
	if !strings.Contains(got, "[click](https://example.com)") {
		t.Errorf("htmlTagToWaMd(<a>) = %q, want markdown link", got)
	}
}

func TestHTMLTagToWaMd_BR(t *testing.T) {
	got := htmlTagToWaMd("line1<br>line2")
	if !strings.Contains(got, "\n") {
		t.Errorf("htmlTagToWaMd(<br>) = %q, want newline", got)
	}
}

func TestHTMLTagToWaMd_PlainText(t *testing.T) {
	got := htmlTagToWaMd("plain text")
	if got != "plain text" {
		t.Errorf("htmlTagToWaMd(plain) = %q, want unchanged", got)
	}
}
