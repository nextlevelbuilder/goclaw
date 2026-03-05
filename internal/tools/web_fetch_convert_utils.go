package tools

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func findChild(n *html.Node, tag atom.Atom) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == tag {
			return c
		}
	}
	return nil
}

func findBody(doc *html.Node) *html.Node {
	var find func(*html.Node) *html.Node
	find = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.DataAtom == atom.Body {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := find(c); found != nil {
				return found
			}
		}
		return nil
	}
	if body := find(doc); body != nil {
		return body
	}
	return doc
}

func collapseWhitespace(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !inSpace {
				buf.WriteByte(' ')
				inSpace = true
			}
		} else {
			buf.WriteRune(r)
			inSpace = false
		}
	}
	return buf.String()
}

func (c *converter) ensureNewline() {
	if c.buf.Len() == 0 {
		return
	}
	s := c.buf.String()
	if s[len(s)-1] != '\n' {
		c.buf.WriteByte('\n')
	}
}

func (c *converter) ensureDoubleNewline() {
	if c.buf.Len() == 0 {
		return
	}
	s := c.buf.String()
	if len(s) >= 2 && s[len(s)-1] == '\n' && s[len(s)-2] == '\n' {
		return
	}
	if s[len(s)-1] == '\n' {
		c.buf.WriteByte('\n')
	} else {
		c.buf.WriteString("\n\n")
	}
}

// collectTableRows extracts rows from a table node. Each row is a slice of cell strings.
func collectTableRows(table *html.Node, mode convertMode) [][]string {
	var rows [][]string
	var findRows func(*html.Node)
	findRows = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Tr {
			var cells []string
			for td := n.FirstChild; td != nil; td = td.NextSibling {
				if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
					sub := &converter{mode: mode}
					sub.walkChildren(td)
					cells = append(cells, strings.TrimSpace(sub.buf.String()))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findRows(c)
		}
	}
	findRows(table)
	return rows
}

// --- output cleanup ---

var reMultiNL = regexp.MustCompile(`\n{3,}`)

func cleanOutput(s string) string {
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func cleanTextOutput(s string) string {
	lines := strings.Split(s, "\n")
	var clean []string
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		clean = append(clean, line)
	}
	s = strings.Join(clean, "\n")
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// stripTagsFallback is a last-resort fallback if the HTML parser fails.
var reStripTags = regexp.MustCompile(`<[^>]+>`)

func stripTagsFallback(s string) string {
	return strings.TrimSpace(reStripTags.ReplaceAllString(s, ""))
}

// markdownToText strips markdown formatting for text mode.
func markdownToText(md string) string {
	s := md
	s = regexp.MustCompile(`(?m)^#{1,6}\s+`).ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = regexp.MustCompile("`[^`]+`").ReplaceAllStringFunc(s, func(m string) string {
		return strings.Trim(m, "`")
	})
	s = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`).ReplaceAllString(s, "$1")
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
