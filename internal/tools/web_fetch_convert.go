package tools

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// extractJSON pretty-prints JSON content.
func extractJSON(body []byte) (string, string) {
	var data any
	if err := json.Unmarshal(body, &data); err == nil {
		formatted, _ := json.MarshalIndent(data, "", "  ")
		return string(formatted), "json"
	}
	return string(body), "raw"
}

// --- DOM-based HTML extraction ---

type convertMode int

const (
	modeMarkdown convertMode = iota
	modeText
)

// converter walks a parsed HTML DOM tree and emits markdown or plain text.
type converter struct {
	buf       strings.Builder
	mode      convertMode
	inPre     bool
	listDepth int
	listType  []atom.Atom // stack: atom.Ul / atom.Ol
	listIndex []int       // ordered list counters
	inLink    bool
}

// Elements to skip entirely (element + all descendants).
var skipElements = map[atom.Atom]bool{
	atom.Head:     true,
	atom.Script:   true,
	atom.Style:    true,
	atom.Noscript: true,
	atom.Svg:      true,
	atom.Template: true,
	atom.Iframe:   true,
	atom.Select:   true,
	atom.Option:   true,
	atom.Button:   true,
	atom.Input:    true,
	atom.Form:     true,
	atom.Nav:      true,
	atom.Footer:   true,
	atom.Picture:  true,
	atom.Source:   true,
}

// Additional elements to skip in text mode only.
var skipInTextMode = map[atom.Atom]bool{
	atom.Header: true,
	atom.Aside:  true,
}

// Block elements that need surrounding newlines.
var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.Main: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true, atom.Blockquote: true,
	atom.Pre: true, atom.Ul: true, atom.Ol: true, atom.Li: true,
	atom.Table: true, atom.Tr: true, atom.Hr: true, atom.Dl: true,
	atom.Dt: true, atom.Dd: true, atom.Figure: true, atom.Figcaption: true,
	atom.Details: true, atom.Summary: true, atom.Address: true,
}

// htmlToMarkdown converts HTML to a markdown-like format using DOM parsing.
func htmlToMarkdown(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return stripTagsFallback(rawHTML)
	}
	body := findBody(doc)
	c := &converter{mode: modeMarkdown}
	c.walkChildren(body)
	return cleanOutput(c.buf.String())
}

// htmlToText extracts plain text from HTML content using DOM parsing.
func htmlToText(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return stripTagsFallback(rawHTML)
	}
	body := findBody(doc)
	c := &converter{mode: modeText}
	c.walkChildren(body)
	return cleanTextOutput(c.buf.String())
}

func (c *converter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		c.handleText(n)
		return
	case html.ElementNode:
		// handled below
	case html.DocumentNode:
		c.walkChildren(n)
		return
	default:
		return
	}

	// Skip hidden elements (display:none, hidden attr, aria-hidden, etc.)
	// to prevent hidden-text prompt injection attacks.
	if isHiddenElement(n) {
		return
	}

	tag := n.DataAtom

	if skipElements[tag] {
		return
	}
	if c.mode == modeText && skipInTextMode[tag] {
		return
	}

	switch tag {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		c.handleHeading(n)
	case atom.P:
		c.handleParagraph(n)
	case atom.A:
		c.handleLink(n)
	case atom.Img:
		c.handleImage(n)
	case atom.Pre:
		c.handlePre(n)
	case atom.Code:
		c.handleCode(n)
	case atom.Blockquote:
		c.handleBlockquote(n)
	case atom.Strong, atom.B:
		c.handleStrong(n)
	case atom.Em, atom.I:
		c.handleEmphasis(n)
	case atom.Br:
		c.buf.WriteByte('\n')
	case atom.Hr:
		c.ensureNewline()
		if c.mode == modeMarkdown {
			c.buf.WriteString("---\n")
		}
	case atom.Ul, atom.Ol:
		c.handleList(n)
	case atom.Li:
		c.handleListItem(n)
	case atom.Table:
		c.handleTable(n)
	case atom.Dt:
		c.handleDefinitionTerm(n)
	case atom.Dd:
		c.handleDefinitionDesc(n)
	default:
		if blockElements[tag] {
			c.ensureNewline()
			c.walkChildren(n)
			c.ensureNewline()
		} else {
			c.walkChildren(n)
		}
	}
}

func (c *converter) walkChildren(n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.walk(child)
	}
}
