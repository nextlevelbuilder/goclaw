package telegram

import (
	"strings"
	"testing"
)

// --- markdownToTelegramHTML: core formatting ---

func TestMarkdownToTelegramHTML_Empty(t *testing.T) {
	got := markdownToTelegramHTML("")
	if got != "" {
		t.Errorf("markdownToTelegramHTML(\"\") = %q, want empty", got)
	}
}

func TestMarkdownToTelegramHTML_Bold(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // substring that must appear
	}{
		{"double-star bold", "**bold text**", "<b>bold text</b>"},
		{"double-underscore bold", "__bold text__", "<b>bold text</b>"},
		{"inline bold", "normal **bold** normal", "<b>bold</b>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToTelegramHTML(tt.input)
			if !strings.Contains(got, tt.want) {
				t.Errorf("markdownToTelegramHTML(%q) = %q, want substring %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_Italic(t *testing.T) {
	got := markdownToTelegramHTML("_italic_")
	if !strings.Contains(got, "<i>italic</i>") {
		t.Errorf("markdownToTelegramHTML(_italic_) = %q, want <i>italic</i>", got)
	}
}

func TestMarkdownToTelegramHTML_Strikethrough(t *testing.T) {
	got := markdownToTelegramHTML("~~strike~~")
	if !strings.Contains(got, "<s>strike</s>") {
		t.Errorf("markdownToTelegramHTML(~~strike~~) = %q, want <s>strike</s>", got)
	}
}

func TestMarkdownToTelegramHTML_InlineCode(t *testing.T) {
	got := markdownToTelegramHTML("`myvar`")
	if !strings.Contains(got, "<code>myvar</code>") {
		t.Errorf("markdownToTelegramHTML(`myvar`) = %q, want <code>myvar</code>", got)
	}
}

func TestMarkdownToTelegramHTML_CodeBlock(t *testing.T) {
	got := markdownToTelegramHTML("```\ncode here\n```")
	if !strings.Contains(got, "<pre><code>") {
		t.Errorf("code block should produce <pre><code>, got: %q", got)
	}
	if !strings.Contains(got, "code here") {
		t.Errorf("code block content missing in: %q", got)
	}
}

func TestMarkdownToTelegramHTML_CodeBlockWithLanguage(t *testing.T) {
	got := markdownToTelegramHTML("```python\nprint('hi')\n```")
	if !strings.Contains(got, "<pre><code>") {
		t.Errorf("code block with language should produce <pre><code>, got: %q", got)
	}
	if !strings.Contains(got, "print") {
		t.Errorf("code content missing, got: %q", got)
	}
}

func TestMarkdownToTelegramHTML_Headers(t *testing.T) {
	// Headers should be stripped (no native header in Telegram HTML).
	tests := []struct {
		input string
		deny  string // must NOT contain raw markdown header
		want  string // should contain the text
	}{
		{"# Header One", "#", "Header One"},
		{"## Sub Header", "##", "Sub Header"},
		{"### H3", "###", "H3"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := markdownToTelegramHTML(tt.input)
			if strings.Contains(got, tt.deny) {
				t.Errorf("header marker %q should be stripped, got: %q", tt.deny, got)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("header text %q missing in: %q", tt.want, got)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_Blockquote(t *testing.T) {
	// Blockquotes should be stripped.
	got := markdownToTelegramHTML("> quoted text")
	if strings.Contains(got, "&gt;") {
		t.Errorf("blockquote > should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "quoted text") {
		t.Errorf("blockquote text should remain, got: %q", got)
	}
}

func TestMarkdownToTelegramHTML_ListItems(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"- item one", "• item one"},
		{"* item two", "• item two"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := markdownToTelegramHTML(tt.input)
			if !strings.Contains(got, tt.want) {
				t.Errorf("markdownToTelegramHTML(%q) = %q, want substring %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownToTelegramHTML_Link(t *testing.T) {
	got := markdownToTelegramHTML("[click here](https://example.com)")
	if !strings.Contains(got, `href="https://example.com"`) {
		t.Errorf("link should produce href, got: %q", got)
	}
	if !strings.Contains(got, "click here") {
		t.Errorf("link text missing in: %q", got)
	}
}

func TestMarkdownToTelegramHTML_HTMLEscaping(t *testing.T) {
	// Plain text with special chars should be escaped.
	got := markdownToTelegramHTML("a & b < c > d")
	if !strings.Contains(got, "&amp;") {
		t.Errorf("& should be escaped, got: %q", got)
	}
	if !strings.Contains(got, "&lt;") {
		t.Errorf("< should be escaped, got: %q", got)
	}
	if !strings.Contains(got, "&gt;") {
		t.Errorf("> should be escaped, got: %q", got)
	}
}

func TestMarkdownToTelegramHTML_HTMLTagsConvertedFirst(t *testing.T) {
	// LLM-emitted HTML tags should be converted to markdown first, then re-rendered as Telegram HTML.
	// <b> → **bold** → <b>bold</b>; <i> → _italic_ → <i>italic</i>, etc.
	t.Run("html bold tag round-trips", func(t *testing.T) {
		got := markdownToTelegramHTML("<b>bold</b>")
		if !strings.Contains(got, "<b>bold</b>") {
			t.Errorf("<b>bold</b> should remain bold in output, got: %q", got)
		}
	})
	t.Run("html italic tag round-trips", func(t *testing.T) {
		// <i>italic</i> → _italic_ → <i>italic</i> through the full pipeline.
		got := markdownToTelegramHTML("<i>italic</i>")
		if !strings.Contains(got, "<i>italic</i>") {
			t.Errorf("<i>italic</i> should remain italic in output, got: %q", got)
		}
	})
	t.Run("html em tag round-trips", func(t *testing.T) {
		got := markdownToTelegramHTML("<em>emphasis</em>")
		if !strings.Contains(got, "<i>emphasis</i>") {
			t.Errorf("<em>emphasis</em> should become <i>emphasis</i>, got: %q", got)
		}
	})
	t.Run("html strike tag round-trips", func(t *testing.T) {
		got := markdownToTelegramHTML("<s>struck</s>")
		if !strings.Contains(got, "<s>struck</s>") {
			t.Errorf("<s>struck</s> should remain as strike in output, got: %q", got)
		}
	})
	t.Run("html code tag round-trips", func(t *testing.T) {
		got := markdownToTelegramHTML("<code>var</code>")
		if !strings.Contains(got, "<code>var</code>") {
			t.Errorf("<code>var</code> should produce code element, got: %q", got)
		}
	})
	t.Run("html br produces newline", func(t *testing.T) {
		got := markdownToTelegramHTML("line1<br>line2")
		if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
			t.Errorf("both lines should appear after <br> conversion, got: %q", got)
		}
	})
}

// --- extractMarkdownTables ---

func TestExtractMarkdownTables_Simple(t *testing.T) {
	input := "| Col1 | Col2 |\n|------|------|\n| A | B |"
	tm := extractMarkdownTables(input)

	if len(tm.rendered) != 1 {
		t.Fatalf("expected 1 table, got %d", len(tm.rendered))
	}
	if !strings.Contains(tm.text, "\x00TB0\x00") {
		t.Errorf("expected TB0 placeholder in text, got: %q", tm.text)
	}
}

func TestExtractMarkdownTables_NoTable(t *testing.T) {
	input := "just plain text\nno table here"
	tm := extractMarkdownTables(input)

	if len(tm.rendered) != 0 {
		t.Errorf("expected 0 tables, got %d", len(tm.rendered))
	}
	if tm.text != input {
		t.Errorf("text without table should be unchanged, got: %q", tm.text)
	}
}

func TestExtractMarkdownTables_MultipleTables(t *testing.T) {
	input := "| A | B |\n|---|---|\n| 1 | 2 |\n\nText\n\n| C | D |\n|---|---|\n| 3 | 4 |"
	tm := extractMarkdownTables(input)

	if len(tm.rendered) != 2 {
		t.Errorf("expected 2 tables, got %d", len(tm.rendered))
	}
}

func TestExtractMarkdownTables_TableInTelegramHTML(t *testing.T) {
	// End-to-end: table in final HTML should be inside <pre> not <pre><code>.
	input := "| Name | Value |\n|------|-------|\n| foo | bar |"
	got := markdownToTelegramHTML(input)

	if !strings.Contains(got, "<pre>") {
		t.Errorf("table should be wrapped in <pre>, got: %q", got)
	}
	if strings.Contains(got, "<pre><code>") {
		t.Errorf("table should NOT use <pre><code>, got: %q", got)
	}
}

// --- renderTableAsCode ---

func TestRenderTableAsCode_Basic(t *testing.T) {
	lines := []string{
		"| Name | Score |",
		"|------|-------|",
		"| Alice | 100 |",
		"| Bob | 85 |",
	}
	result := renderTableAsCode(lines)
	resultLines := strings.Split(result, "\n")

	if len(resultLines) < 4 { // header + sep + 2 data rows
		t.Fatalf("expected at least 4 output lines, got %d: %q", len(resultLines), result)
	}

	// All rows should start and end with |.
	for i, line := range resultLines {
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			t.Errorf("line %d should start and end with |, got: %q", i, line)
		}
	}

	// All rows should have equal display width.
	headerWidth := displayWidth(resultLines[0])
	for i := 1; i < len(resultLines); i++ {
		if displayWidth(resultLines[i]) != headerWidth {
			t.Errorf("row %d width %d != header width %d\nrow: %q", i, displayWidth(resultLines[i]), headerWidth, resultLines[i])
		}
	}
}

func TestRenderTableAsCode_TooFewLines(t *testing.T) {
	// Less than 2 lines → return as-is.
	lines := []string{"| Only header |"}
	result := renderTableAsCode(lines)
	if result != "| Only header |" {
		t.Errorf("single-line table should return as-is, got: %q", result)
	}
}

// --- parseTableRow ---

func TestParseTableRow(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"| A | B | C |", []string{"A", "B", "C"}},
		{"| **Bold** | _italic_ |", []string{"Bold", "italic"}},
		{"| `code` | normal |", []string{"code", "normal"}},
		{"| ~~strike~~ | text |", []string{"strike", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseTableRow(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseTableRow(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cell[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- stripInlineMarkdown ---

func TestStripInlineMarkdown(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"**bold**", "bold"},
		{"__bold__", "bold"},
		{"_italic_", "italic"},
		{"*italic*", "italic"},
		{"~~strike~~", "strike"},
		{"`code`", "code"},
		{"plain text", "plain text"},
		{"**bold** and _italic_", "bold and italic"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripInlineMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("stripInlineMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- escapeHTML ---

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a & b", "a &amp; b"},
		{"a < b", "a &lt; b"},
		{"a > b", "a &gt; b"},
		{"a & b < c > d", "a &amp; b &lt; c &gt; d"},
		{"no special chars", "no special chars"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeHTML(tt.input)
			if got != tt.want {
				t.Errorf("escapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- chunkHTML: additional cases ---

func TestChunkHTML_ExactFit(t *testing.T) {
	text := "hello"
	got := chunkHTML(text, 5)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("chunkHTML exact fit = %v, want [hello]", got)
	}
}

func TestChunkHTML_EmptyString(t *testing.T) {
	got := chunkHTML("", 100)
	if len(got) != 1 || got[0] != "" {
		t.Errorf("chunkHTML(\"\") = %v, want [\"\"]", got)
	}
}

func TestChunkHTML_PrefersParagraphBreak(t *testing.T) {
	// "para1\n\npara2" — should split at \n\n if maxLen allows.
	input := "para1\n\npara2\n\npara3"
	got := chunkHTML(input, 10)
	// Each chunk should not exceed 10 chars.
	for _, chunk := range got {
		if len(chunk) > 10 {
			t.Errorf("chunk %q exceeds maxLen 10", chunk)
		}
	}
}

func TestChunkHTML_PreservesAllContent(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog"
	got := chunkHTML(input, 10)
	// Rejoin and verify content (allowing boundary trimming of spaces).
	joined := strings.Join(got, " ")
	for _, word := range []string{"quick", "brown", "fox", "jumps", "lazy", "dog"} {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q lost in chunking: %v", word, got)
		}
	}
}

// --- chunkPlainText: delegates to chunkHTML ---

func TestChunkPlainText_Delegates(t *testing.T) {
	// chunkPlainText is an alias for chunkHTML.
	input := "line one\nline two\nline three"
	got := chunkPlainText(input, 15)
	want := chunkHTML(input, 15)
	if len(got) != len(want) {
		t.Errorf("chunkPlainText != chunkHTML result")
	}
}

// --- htmlTagToMarkdown ---

func TestHTMLTagToMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		wantSubs []string // substrings that must appear in output
	}{
		{"<br>", []string{"\n"}},
		{"<br/>", []string{"\n"}},
		{"<b>bold</b>", []string{"**bold**"}},
		{"<strong>bold</strong>", []string{"**bold**"}},
		{"<i>italic</i>", []string{"_italic_"}},
		{"<em>italic</em>", []string{"_italic_"}},
		{"<s>struck</s>", []string{"~~struck~~"}},
		{"<del>deleted</del>", []string{"~~deleted~~"}},
		{"<code>var</code>", []string{"`var`"}},
		{`<a href="https://example.com">link</a>`, []string{"[link](https://example.com)"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := htmlTagToMarkdown(tt.input)
			for _, sub := range tt.wantSubs {
				if !strings.Contains(got, sub) {
					t.Errorf("htmlTagToMarkdown(%q) = %q, missing %q", tt.input, got, sub)
				}
			}
		})
	}
}
