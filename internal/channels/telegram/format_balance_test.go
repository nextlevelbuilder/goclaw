package telegram

import (
	"strings"
	"testing"
)

func TestBalanceMarkdownMarker_Bold(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"balanced", "hello **world** ok", "hello **world** ok"},
		{"unclosed at end (the Gia Hân bug)", "• Anh thực nhận: **70", "• Anh thực nhận: 70"},
		{"empty", "", ""},
		{"no marker", "plain text", "plain text"},
		{"one pair only", "**bold**", "**bold**"},
		{"three markers — strip last", "**a**b**c", "**a**bc"},
		{"unclosed with surrounding text", "start **oops end", "start oops end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := balanceMarkdownMarker(tt.input, "**")
			if got != tt.want {
				t.Errorf("balanceMarkdownMarker(%q, **) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBalanceMarkdownMarker_Underline(t *testing.T) {
	// __x__ is also bold in Telegram markdown. Ensure the same parity logic applies.
	got := balanceMarkdownMarker("hey __unclosed", "__")
	if got != "hey unclosed" {
		t.Errorf("balanceMarkdownMarker underline: got %q", got)
	}
}

func TestMarkdownToTelegramHTML_UnclosedBold(t *testing.T) {
	// Reproduces the Gia Hân bug: LLM output cut mid-bold → previously leaked
	// literal `**70` to user; now balancer strips the dangling marker.
	in := "• Tên liên hệ: Bryan\n• Anh thực nhận: **70"
	out := markdownToTelegramHTML(in)
	if strings.Contains(out, "**") {
		t.Fatalf("markdownToTelegramHTML should strip unclosed **, got: %q", out)
	}
}

func TestCloseUnclosedInlineTags(t *testing.T) {
	tests := []struct {
		name        string
		chunk       string
		wantClosed  string
		wantReopen  []string
	}{
		{
			name:       "no tags",
			chunk:      "plain text",
			wantClosed: "plain text",
			wantReopen: nil,
		},
		{
			name:       "balanced bold",
			chunk:      "a <b>bold</b> b",
			wantClosed: "a <b>bold</b> b",
			wantReopen: nil,
		},
		{
			name:       "unclosed bold — close and mark for reopen",
			chunk:      "start <b>bold text",
			wantClosed: "start <b>bold text</b>",
			wantReopen: []string{"b"},
		},
		{
			name:       "nested unclosed",
			chunk:      "<b>bold <i>italic text",
			wantClosed: "<b>bold <i>italic text</i></b>",
			wantReopen: []string{"b", "i"},
		},
		{
			name:       "code with attribute is still tracked",
			chunk:      "use <code>foo",
			wantClosed: "use <code>foo</code>",
			wantReopen: []string{"code"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClosed, gotReopen := closeUnclosedInlineTags(tt.chunk)
			if gotClosed != tt.wantClosed {
				t.Errorf("closed mismatch:\n  got:  %q\n  want: %q", gotClosed, tt.wantClosed)
			}
			if !equalStrings(gotReopen, tt.wantReopen) {
				t.Errorf("reopen mismatch: got %v, want %v", gotReopen, tt.wantReopen)
			}
		})
	}
}

func TestChunkHTML_BalancesTagsAcrossChunks(t *testing.T) {
	// A long bold paragraph split across chunks. Before the fix, chunk 1 ended
	// with an unclosed `<b>` and chunk 2 had an orphan `</b>` — Telegram rejects.
	// After the fix: chunk 1 is closed, chunk 2 re-opens.
	left := "<b>" + strings.Repeat("a", 100)
	right := strings.Repeat("b", 100) + "</b>"
	text := left + "\n\n" + right

	chunks := chunkHTML(text, 120)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		opens := strings.Count(c, "<b>")
		closes := strings.Count(c, "</b>")
		if opens != closes {
			t.Errorf("chunk %d has unbalanced <b>: opens=%d closes=%d\n  content: %q", i, opens, closes, c)
		}
	}
}

func TestChunkHTML_PreservesBalancedSingleChunk(t *testing.T) {
	// Single chunk that already balances should pass through unchanged.
	text := "<b>short and balanced</b>"
	chunks := chunkHTML(text, 4096)
	if len(chunks) != 1 || chunks[0] != text {
		t.Errorf("single chunk should pass through unchanged, got: %v", chunks)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
