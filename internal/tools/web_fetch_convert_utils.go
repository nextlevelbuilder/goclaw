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

// hiddenClasses contains CSS class names used by popular frameworks to hide elements.
// These are well-known, stable class names unlikely to appear in non-hiding contexts.
// Only base "always hidden" classes — no responsive breakpoint variants (hidden-xs, d-md-none).
var hiddenClasses = map[string]bool{
	// Tailwind CSS (v3, v4)
	"hidden":    true, // display: none
	"invisible": true, // visibility: hidden
	"collapse":  true, // visibility: collapse
	"sr-only":   true, // screen-reader only (clip + fixed position)
	// Bootstrap (v3, v4, v5)
	"d-none":                    true, // display: none
	"visually-hidden":           true, // clip-based sr-only (Bootstrap 5)
	"visually-hidden-focusable": false, // NOT hidden — becomes visible on focus
	"sr-only-focusable":         false, // NOT hidden — becomes visible on focus
	// Bulma
	"is-hidden":    true, // display: none
	"is-invisible": true, // visibility: hidden
	"is-sr-only":   true, // screen-reader only
	// Foundation (Zurb)
	"hide":        true, // display: none
	"show-for-sr": true, // screen-reader only
	// UIKit
	"uk-hidden":    true, // display: none
	"uk-invisible": true, // visibility: hidden
	// Materialize CSS
	// "hide" already listed under Foundation
	// Spectre.css
	"d-hide":          true, // display: none (alias of d-none)
	"d-invisible":     true, // visibility: hidden
	"text-hide":       true, // text-indent off-screen
	"text-assistive":  true, // clip-based sr-only
	// Tachyons CSS
	"clip":       true, // clip + fixed position
	"dn":         true, // display: none
	"vis-hidden": true, // visibility: hidden
	// WordPress
	"screen-reader-text": true, // clip-based sr-only
	// Angular Material / CDK
	"cdk-visually-hidden": true, // clip-based sr-only
	// General conventions
	"offscreen": true, // position off-screen
	"clip-hide": true, // clip-based hiding
}

// hasHiddenClass checks if any CSS class on the element matches known hidden class names.
func hasHiddenClass(n *html.Node) bool {
	classAttr := getAttr(n, "class")
	if classAttr == "" {
		return false
	}
	for _, cls := range strings.Fields(classAttr) {
		if hiddenClasses[strings.ToLower(cls)] {
			return true
		}
	}
	return false
}

// isHiddenElement detects elements hidden via HTML attributes, CSS classes, or inline CSS.
// Skips these elements and all descendants to prevent hidden-text injection attacks.
func isHiddenElement(n *html.Node) bool {
	// HTML5 hidden attribute
	for _, a := range n.Attr {
		if a.Key == "hidden" {
			return true
		}
	}
	// aria-hidden="true"
	if getAttr(n, "aria-hidden") == "true" {
		return true
	}
	// Known hidden CSS classes from popular frameworks
	if hasHiddenClass(n) {
		return true
	}
	// Inline style checks
	style := strings.ToLower(getAttr(n, "style"))
	if style == "" {
		return false
	}
	if strings.Contains(style, "display") && strings.Contains(style, "none") {
		return true
	}
	if strings.Contains(style, "visibility") && strings.Contains(style, "hidden") {
		return true
	}
	// Off-screen positioning
	if reOffScreen.MatchString(style) {
		return true
	}
	// Zero font-size
	if strings.Contains(style, "font-size") &&
		(strings.Contains(style, ":0") || strings.Contains(style, ": 0")) {
		return true
	}
	// Zero opacity
	if strings.Contains(style, "opacity") &&
		(strings.Contains(style, ":0") || strings.Contains(style, ": 0")) {
		return true
	}
	return false
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

// reOffScreen matches negative positions commonly used to push elements off-screen.
// Matches -5000 and above (5+ digit negatives, or 4-digit starting with 5-9).
// Lower values like -1000 can be legitimate CSS (animations, slide menus).
var reOffScreen = regexp.MustCompile(`-[5-9]\d{3,}|-\d{5,}`)

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
