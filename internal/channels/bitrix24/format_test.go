package bitrix24

import (
	"strings"
	"testing"
)

func TestBxConvertUserMentionsToReadable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no_bbcode", "plain text", "plain text"},
		{
			"single_user_mention",
			"[USER=62]Đặng Văn Tình[/USER] hello",
			"@Đặng Văn Tình (ID:62) hello",
		},
		{
			"two_user_mentions",
			"[USER=982]Ngân Nguyệt - Hàn Lập[/USER] [USER=62]Đặng Văn Tình[/USER] Đây này em",
			"@Ngân Nguyệt - Hàn Lập (ID:982) @Đặng Văn Tình (ID:62) Đây này em",
		},
		{
			"bot_variant",
			"[BOT=200]Helper Bot[/BOT] please",
			"@Helper Bot (ID:200) please",
		},
		{
			"empty_display_name_falls_back",
			"[USER=62][/USER] hi",
			"@user-62 hi",
		},
		{
			"mismatched_close_tag_tolerated",
			"[USER=62]X[/BOT] ok", // some Bitrix clients mix closers
			"@X (ID:62) ok",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bxConvertUserMentionsToReadable(tc.in); got != tc.want {
				t.Errorf("in=%q\n got=%q\nwant=%q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMarkdownToBitrixBBCode_Empty(t *testing.T) {
	if got := markdownToBitrixBBCode(""); got != "" {
		t.Errorf("empty input → %q, want empty", got)
	}
}

func TestMarkdownToBitrixBBCode_Bold(t *testing.T) {
	cases := map[string]string{
		"hello **world** foo":           "hello [b]world[/b] foo",
		"__bold__ text":                 "[b]bold[/b] text",
		"**a** and **b**":               "[b]a[/b] and [b]b[/b]",
		"no_bold_here underscores stay": "no_bold_here underscores stay",
	}
	for in, want := range cases {
		if got := markdownToBitrixBBCode(in); got != want {
			t.Errorf("in=%q\n got=%q\nwant=%q", in, got, want)
		}
	}
}

func TestMarkdownToBitrixBBCode_Italic(t *testing.T) {
	cases := map[string]string{
		"this is *italic* text": "this is [i]italic[/i] text",
		"this is _italic_ text": "this is [i]italic[/i] text",
		"snake_case_var stays":  "snake_case_var stays",
		"word*star in middle":   "word*star in middle", // no trailing marker → no match
	}
	for in, want := range cases {
		if got := markdownToBitrixBBCode(in); got != want {
			t.Errorf("in=%q\n got=%q\nwant=%q", in, got, want)
		}
	}
}

func TestMarkdownToBitrixBBCode_BoldBeatsItalic(t *testing.T) {
	// Bold pattern `**x**` must NOT be consumed by italic pattern `*x*`.
	in := "Try **really important** now"
	want := "Try [b]really important[/b] now"
	if got := markdownToBitrixBBCode(in); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_Strikethrough(t *testing.T) {
	if got := markdownToBitrixBBCode("~~gone~~ now"); got != "[s]gone[/s] now" {
		t.Errorf("got=%q", got)
	}
}

func TestMarkdownToBitrixBBCode_Links(t *testing.T) {
	cases := map[string]string{
		"[click](https://example.com)":            "[url=https://example.com]click[/url]",
		"See [docs](https://docs.example.com) ok": "See [url=https://docs.example.com]docs[/url] ok",
		"![alt](http://img.example/x.png)":        "[url=http://img.example/x.png]alt[/url]",
	}
	for in, want := range cases {
		if got := markdownToBitrixBBCode(in); got != want {
			t.Errorf("in=%q\n got=%q\nwant=%q", in, got, want)
		}
	}
}

func TestMarkdownToBitrixBBCode_Headers(t *testing.T) {
	in := "# Big\n## Medium\n### Small\nbody"
	want := "[b]Big[/b]\n[b]Medium[/b]\n[b]Small[/b]\nbody"
	if got := markdownToBitrixBBCode(in); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_InlineCode(t *testing.T) {
	in := "Run `go test` in repo"
	want := "Run [i]go test[/i] in repo"
	if got := markdownToBitrixBBCode(in); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_InlineCodeProtectsMarkdown(t *testing.T) {
	// ** inside backticks is literal, must survive conversion.
	in := "Use `**bold**` syntax"
	want := "Use [i]**bold**[/i] syntax"
	if got := markdownToBitrixBBCode(in); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_FencedCodeBlock(t *testing.T) {
	in := "Run this:\n```go\nfunc main() {}\n```\ndone"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "[code]\nfunc main() {}\n[/code]") {
		t.Errorf("fenced block not preserved, got=%q", got)
	}
	// Language hint "go" must be dropped.
	if strings.Contains(got, "```go") {
		t.Errorf("lang hint leaked into output: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_FencedProtectsInnerMarkdown(t *testing.T) {
	// Markdown inside a fenced block is literal.
	in := "```\n**not bold** *not italic*\n```"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "**not bold**") || !strings.Contains(got, "*not italic*") {
		t.Errorf("inner markers were modified: %q", got)
	}
	if strings.Contains(got, "[b]") || strings.Contains(got, "[i]") {
		t.Errorf("markdown inside code was converted: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_UnorderedList(t *testing.T) {
	in := "- apple\n- banana\n* cherry\n+ date"
	got := markdownToBitrixBBCode(in)
	want := "• apple\n• banana\n• cherry\n• date"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_OrderedList(t *testing.T) {
	in := "1. first\n2. second"
	got := markdownToBitrixBBCode(in)
	if got != in {
		t.Errorf("ordered list should pass through: got=%q", got)
	}
}

func TestMarkdownToBitrixBBCode_Blockquote(t *testing.T) {
	in := "> quoted line one\n> quoted line two\nregular line"
	got := markdownToBitrixBBCode(in)
	want := "[quote]quoted line one\nquoted line two[/quote]\nregular line"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_HorizontalRule(t *testing.T) {
	in := "before\n---\nafter"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "────") {
		t.Errorf("horizontal rule missing: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_Table(t *testing.T) {
	in := "| A | B |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |"
	got := markdownToBitrixBBCode(in)
	if strings.Contains(got, "[table]") {
		t.Errorf("Bitrix chat does not render [table]; got: %q", got)
	}
	if !strings.Contains(got, "• [b]A[/b]: 1") || !strings.Contains(got, "— [b]B[/b]: 2") {
		t.Errorf("expected labeled bullet rows: %q", got)
	}
	if !strings.Contains(got, "• [b]A[/b]: 3") {
		t.Errorf("second body row missing: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_TableBorderless(t *testing.T) {
	in := "Nhóm | Helper\n------|--------\ncrm/write | `createDeal`"
	got := markdownToBitrixBBCode(in)
	if strings.Contains(got, "[table]") {
		t.Errorf("borderless table must not use [table]: %q", got)
	}
	if !strings.Contains(got, "• [b]Nhóm[/b]: crm/write") || !strings.Contains(got, "— [b]Helper[/b]: [i]createDeal[/i]") {
		t.Errorf("expected labeled bullets: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_TableOutsideCodeOnly(t *testing.T) {
	in := "```md\n| A | B |\n|---|---|\n| 1 | 2 |\n```\n\n| H1 | H2 |\n|----|----|\n| x  | y  |"
	got := markdownToBitrixBBCode(in)

	if !strings.Contains(got, "[code]\n| A | B |\n|---|---|\n| 1 | 2 |\n[/code]") {
		t.Errorf("fenced table should stay in [code] literal block: %q", got)
	}
	if strings.Contains(got, "[table]") {
		t.Errorf("normal markdown table should not use [table] BBCode: %q", got)
	}
	if !strings.Contains(got, "• [b]H1[/b]: x") {
		t.Errorf("expected labeled list for outer table: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_TableFallbackTextGrid(t *testing.T) {
	// Separator row has 3 columns but header has 2. Extractor still matches,
	// parser rejects it, renderer should fallback to plain text grid.
	in := "| A | B |\n|---|---|---|\n| 1 | 2 |"
	got := markdownToBitrixBBCode(in)
	if strings.Contains(got, "[table]") || strings.Contains(got, "[code]") {
		t.Errorf("fallback should not use table/code tags: %q", got)
	}
	if !strings.Contains(got, "A | B") || !strings.Contains(got, "1 | 2") {
		t.Errorf("fallback grid content missing: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_TableWithInlineMarkdown(t *testing.T) {
	in := "| Col |\n|-----|\n| **bold** [link](https://example.com) `x` |\n| _i_ ~~s~~ |"
	got := markdownToBitrixBBCode(in)
	mustContain := []string{
		"• [b]Col[/b]:",
		"[b]bold[/b]",
		"[url=https://example.com]link[/url]",
		"[i]x[/i]",
		"[i]i[/i]",
		"[s]s[/s]",
	}
	if strings.Contains(got, "[table]") {
		t.Errorf("must not emit [table]: %q", got)
	}
	for _, m := range mustContain {
		if !strings.Contains(got, m) {
			t.Errorf("missing %q in: %q", m, got)
		}
	}
}

func TestMarkdownToBitrixBBCode_HTMLFromLLM(t *testing.T) {
	// LLMs occasionally emit raw HTML; those tags should be normalised
	// through the Markdown pipeline into BBCode, not leak as literal tags.
	in := "Hello <b>world</b> and <i>italic</i> with <code>inline</code>."
	got := markdownToBitrixBBCode(in)
	want := "Hello [b]world[/b] and [i]italic[/i] with [i]inline[/i]."
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_HTMLLink(t *testing.T) {
	in := `See <a href="https://x.example">here</a> please.`
	got := markdownToBitrixBBCode(in)
	want := "See [url=https://x.example]here[/url] please."
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_CollapseBlankLines(t *testing.T) {
	in := "one\n\n\n\n\ntwo"
	got := markdownToBitrixBBCode(in)
	want := "one\n\ntwo"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_TrimsOuterWhitespace(t *testing.T) {
	in := "\n\n  hello  \n\n"
	got := markdownToBitrixBBCode(in)
	want := "hello"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_MixedExample(t *testing.T) {
	// Realistic multi-feature LLM reply — smoke test that none of the
	// transforms clobber each other.
	in := `# Kết quả

Đây là **tóm tắt** nhanh cho bạn:

- điểm *quan trọng* số 1
- điểm thứ 2 với ` + "`code`" + `

Xem thêm ở [trang tài liệu](https://docs.example.vn).

` + "```python\ndef hello():\n    print(\"hi\")\n```" + `

> Lưu ý: áp dụng cho v2.`

	got := markdownToBitrixBBCode(in)

	// Spot-check landmarks rather than exact equality — regex ordering of
	// transforms is an implementation detail, but these markers must appear.
	// Note the bullet line: by the time we check, the italic pass has
	// already rewritten *quan trọng* → [i]quan trọng[/i], so we assert on
	// the post-transform shape.
	mustContain := []string{
		"[b]Kết quả[/b]",
		"[b]tóm tắt[/b]",
		"[i]quan trọng[/i]",
		"• điểm [i]quan trọng[/i] số 1",
		"[url=https://docs.example.vn]trang tài liệu[/url]",
		"[code]\ndef hello():\n    print(\"hi\")\n[/code]",
		"[quote]Lưu ý: áp dụng cho v2.[/quote]",
		"[i]code[/i]",
	}
	for _, m := range mustContain {
		if !strings.Contains(got, m) {
			t.Errorf("missing %q in:\n%s", m, got)
		}
	}
}

// Regression: two italic pairs separated by a single non-word char. The
// italic regex consumes its trailing flanking char, which caused pair #2 to
// be missed in a single pass. markdownToBitrixBBCode now loops to stability.
func TestMarkdownToBitrixBBCode_ItalicAdjacentPairs(t *testing.T) {
	cases := map[string]string{
		"*a* *b*":                 "[i]a[/i] [i]b[/i]",
		"_a_ _b_":                 "[i]a[/i] [i]b[/i]",
		"say *one* *two* *three*": "say [i]one[/i] [i]two[/i] [i]three[/i]",
	}
	for in, want := range cases {
		if got := markdownToBitrixBBCode(in); got != want {
			t.Errorf("\n in=%q\n got=%q\nwant=%q", in, got, want)
		}
	}
}

// Regression: single-line fenced “code“ used to lose `code` as a phantom
// language hint and render [code]\n\n[/code]. Now the prefix group only
// consumes a lang hint when followed by a newline.
func TestMarkdownToBitrixBBCode_FencedSingleLine(t *testing.T) {
	in := "Use ```literal``` mid-sentence"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "[code]\nliteral\n[/code]") {
		t.Errorf("single-line fenced lost content: %q", got)
	}
}

// Inline “ `…` “ → [i] (not [code]); fenced ``` → [code] — matches Bitrix UX (prose vs snippet).
func TestMarkdownToBitrixBBCode_FencedVsInlineIdentifiers(t *testing.T) {
	in := "Tham số `ALLOW_CHANGE_DEADLINE` và `TASK_CONTROL` dùng giá trị `\"Y\"`.\n\n```js\nawait codemode.request({\n  ALLOW_CHANGE_DEADLINE: 'Y'\n});\n```"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "[i]ALLOW_CHANGE_DEADLINE[/i]") || !strings.Contains(got, "[i]TASK_CONTROL[/i]") {
		t.Errorf("inline backticks should become [i], got: %q", got)
	}
	if strings.Contains(got, "[code]ALLOW_CHANGE") {
		t.Errorf("inline must not use [code] wrapper: %q", got)
	}
	if !strings.Contains(got, "[code]\nawait codemode.request({") {
		t.Errorf("fenced block should stay [code]: %q", got)
	}
}

func TestMarkdownToBitrixBBCode_LLMOneLineCodeBBCodeToItalic(t *testing.T) {
	in := "helper [code]createSpaItem[/code] ok"
	want := "helper [i]createSpaItem[/i] ok"
	if got := markdownToBitrixBBCode(in); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_LLMBlockCodeBBCodePreserved(t *testing.T) {
	in := "[code]\n{\n  \"itemId\": 5723\n}\n[/code]"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "[code]\n{\n") || strings.Contains(got, "[i]{\n") {
		t.Errorf("block [code] must be kept, got=%q", got)
	}
}

func TestMarkdownToBitrixBBCode_LLMSameLineOpenMultilineInner(t *testing.T) {
	in := "[code]line1\nline2[/code]"
	got := markdownToBitrixBBCode(in)
	if !strings.Contains(got, "[code]line1") || strings.Contains(got, "[i]line1") {
		t.Errorf("multiline inner must stay [code], got=%q", got)
	}
}

// Regression: placeholder scheme uses \x00…\x00 framing. A literal NUL in
// the LLM output used to collide with our placeholders and corrupt
// restoration. markdownToBitrixBBCode now strips NULs on entry.
func TestMarkdownToBitrixBBCode_StripsNUL(t *testing.T) {
	in := "hel\x00lo **world**"
	got := markdownToBitrixBBCode(in)
	want := "hello [b]world[/b]"
	if got != want {
		t.Errorf("\n got=%q\nwant=%q", got, want)
	}
}

func TestMarkdownToBitrixBBCode_IdempotentOnBBCode(t *testing.T) {
	// If someone already formatted as BBCode, running the converter again
	// must not double-wrap or corrupt the tags.
	in := "[b]already[/b] [url=https://x.io]link[/url]"
	got := markdownToBitrixBBCode(in)
	if got != in {
		t.Errorf("BBCode input was modified: got=%q want=%q", got, in)
	}
}
