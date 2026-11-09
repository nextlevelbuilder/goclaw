package zalo

import "strings"

// StyleSpan is a Zalo text style range. Start and Len are UTF-16 code units,
// matching the JS client and the wire protocol.
type StyleSpan struct {
	Start int
	Len   int
	St    string
}

// MarkdownToZaloStyles converts the supported Markdown subset to plain text
// plus Zalo style spans. Unsupported constructs (tables, nested inline styles)
// degrade to plain text rather than corrupting offsets.
func MarkdownToZaloStyles(md string) (string, []StyleSpan) {
	if md == "" {
		return "", nil
	}

	var tb textBuilder
	lines := strings.Split(md, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			tb.add(line, "")
		} else if inFence {
			tb.add(line, "")
		} else {
			parseLine(line, &tb)
		}
		if i < len(lines)-1 {
			tb.add("\n", "")
		}
	}
	return tb.b.String(), tb.spans
}

type textBuilder struct {
	b     strings.Builder
	utf16 int
	spans []StyleSpan
}

func (tb *textBuilder) add(s, st string) {
	if s == "" {
		return
	}
	start := tb.utf16
	tb.b.WriteString(s)
	tb.utf16 += utf16Len(s)
	if st != "" {
		tb.spans = append(tb.spans, StyleSpan{Start: start, Len: utf16Len(s), St: st})
	}
}

func (tb *textBuilder) addMulti(s string, styles ...string) {
	if s == "" {
		return
	}
	start := tb.utf16
	tb.b.WriteString(s)
	n := utf16Len(s)
	tb.utf16 += n
	for _, st := range styles {
		if st != "" {
			tb.spans = append(tb.spans, StyleSpan{Start: start, Len: n, St: st})
		}
	}
}

func parseLine(line string, tb *textBuilder) {
	trimmed := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(trimmed)]

	switch {
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
		tb.add(indent, "")
		tb.addMulti(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "), "lst_1", "ind_10")
		return
	case strings.HasPrefix(trimmed, "# "):
		tb.add(indent, "")
		tb.addMulti(strings.TrimPrefix(trimmed, "# "), "b", "f_18")
		return
	}

	if rest, ok := orderedListRest(trimmed); ok {
		tb.add(indent, "")
		tb.addMulti(rest, "lst_2", "ind_10")
		return
	}

	tb.add(indent, "")
	parseInline(trimmed, tb)
}

func orderedListRest(s string) (string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(s) || s[i] != '.' || s[i+1] != ' ' {
		return "", false
	}
	return s[i+2:], true
}

func parseInline(s string, tb *textBuilder) {
	for len(s) > 0 {
		if i := strings.Index(s, "**"); i >= 0 {
			if j := strings.Index(s[i+2:], "**"); j >= 0 {
				tb.add(s[:i], "")
				tb.add(s[i+2:i+2+j], "b")
				s = s[i+2+j+2:]
				continue
			}
		}
		if i := strings.Index(s, "~~"); i >= 0 {
			if j := strings.Index(s[i+2:], "~~"); j >= 0 {
				tb.add(s[:i], "")
				tb.add(s[i+2:i+2+j], "s")
				s = s[i+2+j+2:]
				continue
			}
		}
		if i := strings.Index(s, "*"); i >= 0 {
			if j := strings.Index(s[i+1:], "*"); j >= 0 {
				tb.add(s[:i], "")
				tb.add(s[i+1:i+1+j], "i")
				s = s[i+1+j+1:]
				continue
			}
		}
		tb.add(s, "")
		return
	}
}

// utf16Len returns the number of UTF-16 code units, matching JS string length.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
