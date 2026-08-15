package feishu

import (
	"reflect"
	"testing"
)

func TestExtractLarkDocURLs(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []DocRef
	}{
		{
			name:  "no urls",
			input: "just some text without any links",
			want:  nil,
		},
		{
			name:  "single larksuite docx",
			input: "check this https://example.larksuite.com/docx/ABCD1234EF",
			want: []DocRef{
				{DocID: "ABCD1234EF", OriginalURL: "https://example.larksuite.com/docx/ABCD1234EF"},
			},
		},
		{
			name:  "single feishu docx",
			input: "看这个 https://example.feishu.cn/docx/XYZ7890abc",
			want: []DocRef{
				{DocID: "XYZ7890abc", OriginalURL: "https://example.feishu.cn/docx/XYZ7890abc"},
			},
		},
		{
			name:  "two docs in text",
			input: "first https://a.larksuite.com/docx/AAA111 then https://b.feishu.cn/docx/BBB222",
			want: []DocRef{
				{DocID: "AAA111", OriginalURL: "https://a.larksuite.com/docx/AAA111"},
				{DocID: "BBB222", OriginalURL: "https://b.feishu.cn/docx/BBB222"},
			},
		},
		{
			name:  "same doc twice deduplicated",
			input: "link https://x.larksuite.com/docx/SAME99 and again https://x.larksuite.com/docx/SAME99",
			want: []DocRef{
				{DocID: "SAME99", OriginalURL: "https://x.larksuite.com/docx/SAME99"},
			},
		},
		{
			name:  "sheets type ignored (not docx)",
			input: "sheet: https://x.larksuite.com/sheets/SHEET123",
			want:  nil,
		},
		{
			name:  "base type ignored",
			input: "base: https://x.larksuite.com/base/BASE123",
			want:  nil,
		},
		{
			name:  "wiki type ignored",
			input: "wiki: https://x.larksuite.com/wiki/WIKI123",
			want:  nil,
		},
		{
			name:  "non-lark domain ignored",
			input: "https://evil.com/docx/ATTACKER",
			want:  nil,
		},
		{
			name:  "trailing punctuation stripped",
			input: "see https://x.larksuite.com/docx/DOC999, nice",
			want: []DocRef{
				{DocID: "DOC999", OriginalURL: "https://x.larksuite.com/docx/DOC999"},
			},
		},
		{
			// Regression: reviewer's lazy-class bypass. With a lazy hostname
			// class, the first URL would match by skipping over "?u=" to the
			// second "/docx/". The strict hostname class now rejects that
			// bypass — the first URL (no /docx/ path) fails; the second URL
			// embedded in the query string is extracted separately as a
			// standalone URL. This IS correct behavior: an attacker can't
			// hide a docx URL here that the user hasn't already visibly
			// pasted. The visible [Lark Doc: <url>] block shows users which
			// doc was actually fetched, and the bot only has access to docs
			// explicitly shared with its app.
			name:  "lazy bypass closed; embedded full url still matches as standalone",
			input: `see https://foo.larksuite.com?u=https://attacker.larksuite.com/docx/EMBED`,
			want: []DocRef{
				{DocID: "EMBED", OriginalURL: "https://attacker.larksuite.com/docx/EMBED"},
			},
		},
		{
			// Regression: nested path prefix before /docx/ must not match —
			// the regex anchors to `/docx/` directly off the host.
			name:  "nested path prefix not matched",
			input: "weird https://foo.larksuite.com/abc/docx/REAL",
			want:  nil,
		},
		{
			// Port in hostname is rare but legal; extractor should tolerate it.
			name:  "hostname with port",
			input: "https://x.larksuite.com:8443/docx/WITHPORT",
			want: []DocRef{
				{DocID: "WITHPORT", OriginalURL: "https://x.larksuite.com:8443/docx/WITHPORT"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractLarkDocURLs(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractLarkDocURLs(%q):\n  got:  %#v\n  want: %#v", tc.input, got, tc.want)
			}
		})
	}
}
