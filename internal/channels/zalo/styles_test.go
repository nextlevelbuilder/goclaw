package zalo

import "testing"

func TestMarkdownToZaloStylesVietnameseAndEmoji(t *testing.T) {
	text, spans := MarkdownToZaloStyles("**Xin chào** 😀 *nghiêng*")
	if text != "Xin chào 😀 nghiêng" {
		t.Fatalf("text = %q", text)
	}
	want := []StyleSpan{
		{Start: 0, Len: 8, St: "b"},
		{Start: 12, Len: 7, St: "i"},
	}
	if len(spans) != len(want) {
		t.Fatalf("spans = %#v, want %#v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("span[%d] = %#v, want %#v", i, spans[i], want[i])
		}
	}
}

func TestMarkdownToZaloStylesEmojiSurrogatePair(t *testing.T) {
	text, spans := MarkdownToZaloStyles("**😀**")
	if text != "😀" {
		t.Fatalf("text = %q", text)
	}
	if len(spans) != 1 || spans[0] != (StyleSpan{Start: 0, Len: 2, St: "b"}) {
		t.Fatalf("spans = %#v, want one 2-unit bold span", spans)
	}
}

func TestMarkdownToZaloStylesListsAndHeading(t *testing.T) {
	text, spans := MarkdownToZaloStyles("- một\n1. hai\n# Tiêu đề")
	if text != "một\nhai\nTiêu đề" {
		t.Fatalf("text = %q", text)
	}
	want := []StyleSpan{
		{Start: 0, Len: 3, St: "lst_1"},
		{Start: 0, Len: 3, St: "ind_10"},
		{Start: 4, Len: 3, St: "lst_2"},
		{Start: 4, Len: 3, St: "ind_10"},
		{Start: 8, Len: 7, St: "b"},
		{Start: 8, Len: 7, St: "f_18"},
	}
	if len(spans) != len(want) {
		t.Fatalf("spans = %#v, want %#v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("span[%d] = %#v, want %#v", i, spans[i], want[i])
		}
	}
}

func TestMarkdownToZaloStylesSpansWithinBounds(t *testing.T) {
	inputs := []string{
		"**bold** and *italic* and ~~strike~~",
		"- item one\n- item two",
		"1. first\n2. second",
		"# Heading\nplain text",
		"😀 **emoji** 🚀",
	}
	for _, in := range inputs {
		text, spans := MarkdownToZaloStyles(in)
		total := utf16Len(text)
		for _, span := range spans {
			if span.Start < 0 || span.Len < 0 || span.Start+span.Len > total {
				t.Fatalf("input %q: span %#v outside text length %d", in, span, total)
			}
		}
	}
}
