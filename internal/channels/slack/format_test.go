package slack

import (
	"testing"
)

func TestMarkdownToSlackMrkdwn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "bold double asterisk",
			input:    "this is **bold** text",
			expected: "this is *bold* text",
		},
		{
			name:     "bold underscore",
			input:    "this is __bold__ text",
			expected: "this is *bold* text",
		},
		{
			name:     "strikethrough",
			input:    "this is ~~striked~~ text",
			expected: "this is ~striked~ text",
		},
		{
			name:     "header h1",
			input:    "# Header Title",
			expected: "*Header Title*",
		},
		{
			name:     "header h2",
			input:    "## Sub Header",
			expected: "*Sub Header*",
		},
		{
			name:     "header h6",
			input:    "###### Small Header",
			expected: "*Small Header*",
		},
		{
			name:     "markdown link",
			input:    "[click here](https://example.com)",
			expected: "<https://example.com|click here>",
		},
		{
			name:     "inline code",
			input:    "use `variable` in code",
			expected: "use `variable` in code",
		},
		{
			name:     "code block",
			input:    "```\nfunction test() {}\n```",
			expected: "```\nfunction test() {}\n```",
		},
		{
			name:     "preserve slack tokens user mention",
			input:    "hey <@U123456> check this",
			expected: "hey <@U123456> check this",
		},
		{
			name:     "preserve slack tokens channel mention",
			input:    "discuss in <#C123456>",
			expected: "discuss in <#C123456>",
		},
		{
			name:     "preserve slack tokens url",
			input:    "see <https://example.com>",
			expected: "see <https://example.com>",
		},
		{
			name:     "preserve slack tokens mailto",
			input:    "email <mailto:user@example.com>",
			expected: "email <mailto:user@example.com>",
		},
		{
			name:     "mixed formatting",
			input:    "**bold** and ~~strike~~ and `code`",
			expected: "*bold* and ~strike~ and `code`",
		},
		{
			name:     "html bold tag",
			input:    "this is <b>bold</b> text",
			expected: "this is *bold* text",
		},
		{
			name:     "html italic tag",
			input:    "this is <i>italic</i> text",
			expected: "this is _italic_ text",
		},
		{
			name:     "html strike tag",
			input:    "this is <s>struck</s> text",
			expected: "this is ~struck~ text",
		},
		{
			name:     "html code tag",
			input:    "use <code>var</code> here",
			expected: "use `var` here",
		},
		{
			name:     "html link tag",
			input:    "click <a href=\"https://example.com\">here</a>",
			expected: "click <https://example.com|here>",
		},
		{
			name:     "html br tag",
			input:    "line1<br>line2",
			expected: "line1\nline2",
		},
		{
			name:     "html p tags",
			input:    "<p>paragraph</p>",
			expected: "\nparagraph\n",
		},
		{
			name:     "complex mixed",
			input:    "# Title\n\nSee **bold** at <https://example.com> and `code`",
			expected: "*Title*\n\nSee *bold* at <https://example.com> and `code`",
		},
		{
			name:     "special chars escaped",
			input:    "a & b < c > d",
			expected: "a &amp; b &lt; c &gt; d",
		},
		{
			name:     "slack tokens with special chars preserved",
			input:    "email <mailto:user+tag@example.com>",
			expected: "email <mailto:user+tag@example.com>",
		},
		{
			name:     "multiple slack tokens",
			input:    "<@U123> and <#C456> with <https://example.com>",
			expected: "<@U123> and <#C456> with <https://example.com>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToSlackMrkdwn(tt.input)
			if got != tt.expected {
				t.Errorf("markdownToSlackMrkdwn(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestEscapeHTMLEntities(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special chars",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "ampersand",
			input:    "a & b",
			expected: "a &amp; b",
		},
		{
			name:     "less than",
			input:    "a < b",
			expected: "a &lt; b",
		},
		{
			name:     "greater than",
			input:    "a > b",
			expected: "a &gt; b",
		},
		{
			name:     "all special chars",
			input:    "a & b < c > d",
			expected: "a &amp; b &lt; c &gt; d",
		},
		{
			name:     "multiple ampersands",
			input:    "a & b & c & d",
			expected: "a &amp; b &amp; c &amp; d",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeHTMLEntities(tt.input)
			if got != tt.expected {
				t.Errorf("escapeHTMLEntities(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractSlackTokens(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedCount   int
		expectedContent string // should have placeholders
	}{
		{
			name:          "no tokens",
			input:         "hello world",
			expectedCount: 0,
		},
		{
			name:          "user mention",
			input:         "hey <@U123456>",
			expectedCount: 1,
		},
		{
			name:          "channel mention",
			input:         "in <#C789012>",
			expectedCount: 1,
		},
		{
			name:          "https url",
			input:         "see <https://example.com>",
			expectedCount: 1,
		},
		{
			name:          "http url",
			input:         "see <http://example.com>",
			expectedCount: 1,
		},
		{
			name:          "ftp url",
			input:         "download <ftp://example.com>",
			expectedCount: 1,
		},
		{
			name:          "mailto",
			input:         "email <mailto:user@example.com>",
			expectedCount: 1,
		},
		{
			name:          "tel",
			input:         "call <tel:+1234567890>",
			expectedCount: 1,
		},
		{
			name:          "multiple tokens",
			input:         "<@U123> and <#C456> with <https://example.com>",
			expectedCount: 3,
		},
		{
			name:          "empty string",
			input:         "",
			expectedCount: 0,
		},
		{
			name:          "user mention with special chars",
			input:         "<@U123456|user.name>",
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, result := extractSlackTokens(tt.input)
			if len(tokens) != tt.expectedCount {
				t.Errorf("extractSlackTokens(%q) returned %d tokens, want %d", tt.input, len(tokens), tt.expectedCount)
			}
			if tt.expectedCount > 0 && result == tt.input {
				t.Errorf("extractSlackTokens(%q) should replace tokens with placeholders, but result = %q", tt.input, result)
			}
		})
	}
}

func TestExtractCodeBlocks(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCount int
	}{
		{
			name:          "no code blocks",
			input:         "hello world",
			expectedCount: 0,
		},
		{
			name:          "single code block",
			input:         "text `code` more",
			expectedCount: 0, // inline, not block
		},
		{
			name:          "paired code block fences",
			input:         "before\n```\ncode\n```\nafter",
			expectedCount: 1,
		},
		{
			name:          "multiple code blocks",
			input:         "```\nblock1\n```\nmiddle\n```\nblock2\n```",
			expectedCount: 2,
		},
		{
			name:          "unpaired fence odd",
			input:         "text\n```\ncode (no closing)",
			expectedCount: 0,
		},
		{
			name:          "empty code block",
			input:         "```\n\n```",
			expectedCount: 1,
		},
		{
			name:          "code with language marker",
			input:         "```python\ncode\n```",
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, result := extractCodeBlocks(tt.input)
			if len(blocks) != tt.expectedCount {
				t.Errorf("extractCodeBlocks(%q) returned %d blocks, want %d", tt.input, len(blocks), tt.expectedCount)
			}
			if tt.expectedCount > 0 && result == tt.input {
				t.Errorf("extractCodeBlocks(%q) should replace blocks with placeholders", tt.input)
			}
		})
	}
}

func TestExtractInlineCodes(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCount int
	}{
		{
			name:          "no inline code",
			input:         "hello world",
			expectedCount: 0,
		},
		{
			name:          "single inline code",
			input:         "use `variable` here",
			expectedCount: 1,
		},
		{
			name:          "multiple inline codes",
			input:         "`var1` and `var2` and `var3`",
			expectedCount: 3,
		},
		{
			name:          "unpaired backtick",
			input:         "use `code",
			expectedCount: 0,
		},
		{
			name:          "empty inline code",
			input:         "use `` here",
			expectedCount: 1,
		},
		{
			name:          "with special chars inside",
			input:         "use `my-var_name` here",
			expectedCount: 1,
		},
		{
			name:          "backticks in code block are also extracted",
			input:         "```\n`inside`\n```",
			expectedCount: 4, // extractInlineCodes doesn't know about code blocks
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codes, result := extractInlineCodes(tt.input)
			if len(codes) != tt.expectedCount {
				t.Errorf("extractInlineCodes(%q) returned %d codes, want %d", tt.input, len(codes), tt.expectedCount)
			}
			if tt.expectedCount > 0 && result == tt.input {
				t.Errorf("extractInlineCodes(%q) should replace codes with placeholders", tt.input)
			}
		})
	}
}

func TestConvertTablesToCodeBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no table",
			input:    "just text",
			expected: "just text",
		},
		{
			name:     "simple table",
			input:    "| Col1 | Col2 |\n|------|------|\n| A | B |",
			expected: "```\n| Col1 | Col2 |\n| A | B |\n```",
		},
		{
			name:     "table with separator stripped",
			input:    "| Header |\n|--------|",
			expected: "```\n| Header |\n```",
		},
		{
			name:     "table at end",
			input:    "text\n| Col |\n|-----|\n| A |",
			expected: "text\n```\n| Col |\n| A |\n```",
		},
		{
			name:     "text after table",
			input:    "| A |\n|---|\nmore text",
			expected: "```\n| A |\n```\nmore text",
		},
		{
			name:     "multiple tables",
			input:    "| A |\n|---|\ntext\n| B |\n|---|",
			expected: "```\n| A |\n```\ntext\n```\n| B |\n```",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTablesToCodeBlocks(tt.input)
			if got != tt.expected {
				t.Errorf("convertTablesToCodeBlocks(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHTMLTagsToMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bold tag",
			input:    "<b>bold</b>",
			expected: "**bold**",
		},
		{
			name:     "strong tag",
			input:    "<strong>strong</strong>",
			expected: "**strong**",
		},
		{
			name:     "italic tag",
			input:    "<i>italic</i>",
			expected: "_italic_",
		},
		{
			name:     "em tag",
			input:    "<em>emphasis</em>",
			expected: "_emphasis_",
		},
		{
			name:     "strike tag",
			input:    "<s>struck</s>",
			expected: "~~struck~~",
		},
		{
			name:     "del tag",
			input:    "<del>deleted</del>",
			expected: "~~deleted~~",
		},
		{
			name:     "code tag",
			input:    "<code>code</code>",
			expected: "`code`",
		},
		{
			name:     "link tag",
			input:    "<a href=\"https://example.com\">link</a>",
			expected: "[link](https://example.com)",
		},
		{
			name:     "br tag",
			input:    "line1<br>line2",
			expected: "line1\nline2",
		},
		{
			name:     "br self-closing tag",
			input:    "line1<br/>line2",
			expected: "line1\nline2",
		},
		{
			name:     "p tag",
			input:    "<p>paragraph</p>",
			expected: "\nparagraph\n",
		},
		{
			name:     "case insensitive",
			input:    "<B>bold</B>",
			expected: "**bold**",
		},
		{
			name:     "multiple tags",
			input:    "<b>bold</b> and <i>italic</i>",
			expected: "**bold** and _italic_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlTagsToMarkdown(tt.input)
			if got != tt.expected {
				t.Errorf("htmlTagsToMarkdown(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
