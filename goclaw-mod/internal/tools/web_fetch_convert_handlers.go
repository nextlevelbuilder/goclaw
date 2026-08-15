package tools

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func (c *converter) handleText(n *html.Node) {
	text := n.Data
	if c.inPre {
		c.buf.WriteString(text)
		return
	}
	text = collapseWhitespace(text)
	if text == "" {
		return
	}
	if text == " " && c.buf.Len() > 0 {
		c.buf.WriteByte(' ')
		return
	}
	c.buf.WriteString(text)
}

func (c *converter) handleHeading(n *html.Node) {
	c.ensureDoubleNewline()
	if c.mode == modeMarkdown && len(n.Data) == 2 && n.Data[0] == 'h' {
		level := int(n.Data[1] - '0')
		for range level {
			c.buf.WriteByte('#')
		}
		c.buf.WriteByte(' ')
	}
	c.walkChildren(n)
	c.buf.WriteByte('\n')
}

func (c *converter) handleParagraph(n *html.Node) {
	c.ensureDoubleNewline()
	c.walkChildren(n)
	c.buf.WriteByte('\n')
}

func (c *converter) handleLink(n *html.Node) {
	href := getAttr(n, "href")
	if c.mode == modeText || c.inLink || href == "" || strings.HasPrefix(href, "javascript:") {
		c.walkChildren(n)
		return
	}
	c.inLink = true
	c.buf.WriteByte('[')
	c.walkChildren(n)
	c.buf.WriteString("](")
	c.buf.WriteString(href)
	c.buf.WriteByte(')')
	c.inLink = false
}

func (c *converter) handleImage(n *html.Node) {
	alt := getAttr(n, "alt")
	src := getAttr(n, "src")
	if c.mode == modeMarkdown {
		c.buf.WriteString("![")
		c.buf.WriteString(alt)
		c.buf.WriteByte(']')
		if src != "" {
			c.buf.WriteByte('(')
			c.buf.WriteString(src)
			c.buf.WriteByte(')')
		}
	} else if alt != "" {
		c.buf.WriteString(alt)
	}
}

func (c *converter) handlePre(n *html.Node) {
	c.ensureDoubleNewline()
	if c.mode == modeMarkdown {
		lang := ""
		if code := findChild(n, atom.Code); code != nil {
			cls := getAttr(code, "class")
			for part := range strings.FieldsSeq(cls) {
				if rest, ok := strings.CutPrefix(part, "language-"); ok {
					lang = rest
					break
				}
				if rest, ok := strings.CutPrefix(part, "lang-"); ok {
					lang = rest
					break
				}
			}
		}
		c.buf.WriteString("```")
		c.buf.WriteString(lang)
		c.buf.WriteByte('\n')
	}
	c.inPre = true
	c.walkChildren(n)
	c.inPre = false
	if c.mode == modeMarkdown {
		c.ensureNewline()
		c.buf.WriteString("```\n")
	} else {
		c.buf.WriteByte('\n')
	}
}

func (c *converter) handleCode(n *html.Node) {
	if c.inPre {
		c.walkChildren(n)
		return
	}
	if c.mode == modeMarkdown {
		c.buf.WriteByte('`')
		c.walkChildren(n)
		c.buf.WriteByte('`')
	} else {
		c.walkChildren(n)
	}
}

func (c *converter) handleBlockquote(n *html.Node) {
	c.ensureDoubleNewline()
	if c.mode == modeMarkdown {
		sub := &converter{mode: c.mode, inPre: c.inPre}
		sub.walkChildren(n)
		for i, line := range strings.Split(strings.TrimSpace(sub.buf.String()), "\n") {
			if i > 0 {
				c.buf.WriteByte('\n')
			}
			c.buf.WriteString("> ")
			c.buf.WriteString(line)
		}
		c.buf.WriteByte('\n')
	} else {
		c.walkChildren(n)
	}
}

func (c *converter) handleStrong(n *html.Node) {
	if c.mode == modeMarkdown {
		c.buf.WriteString("**")
		c.walkChildren(n)
		c.buf.WriteString("**")
	} else {
		c.walkChildren(n)
	}
}

func (c *converter) handleEmphasis(n *html.Node) {
	if c.mode == modeMarkdown {
		c.buf.WriteByte('*')
		c.walkChildren(n)
		c.buf.WriteByte('*')
	} else {
		c.walkChildren(n)
	}
}

func (c *converter) handleList(n *html.Node) {
	c.ensureNewline()
	c.listDepth++
	c.listType = append(c.listType, n.DataAtom)
	c.listIndex = append(c.listIndex, 0)
	c.walkChildren(n)
	c.listDepth--
	c.listType = c.listType[:len(c.listType)-1]
	c.listIndex = c.listIndex[:len(c.listIndex)-1]
	c.ensureNewline()
}

func (c *converter) handleListItem(n *html.Node) {
	c.ensureNewline()
	indent := strings.Repeat("  ", max(0, c.listDepth-1))
	c.buf.WriteString(indent)

	if len(c.listType) > 0 && c.listType[len(c.listType)-1] == atom.Ol {
		idx := len(c.listIndex) - 1
		c.listIndex[idx]++
		fmt.Fprintf(&c.buf, "%d. ", c.listIndex[idx])
	} else {
		c.buf.WriteString("- ")
	}
	c.walkChildren(n)
}

func (c *converter) handleTable(n *html.Node) {
	c.ensureDoubleNewline()
	rows := collectTableRows(n, c.mode)
	if len(rows) == 0 {
		return
	}
	colCount := 0
	for _, row := range rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}
	if c.mode == modeMarkdown {
		for i, row := range rows {
			c.buf.WriteByte('|')
			for j := 0; j < colCount; j++ {
				cell := ""
				if j < len(row) {
					cell = row[j]
				}
				c.buf.WriteByte(' ')
				c.buf.WriteString(cell)
				c.buf.WriteString(" |")
			}
			c.buf.WriteByte('\n')
			if i == 0 {
				c.buf.WriteByte('|')
				for j := 0; j < colCount; j++ {
					c.buf.WriteString(" --- |")
				}
				c.buf.WriteByte('\n')
			}
		}
	} else {
		for _, row := range rows {
			c.buf.WriteString(strings.Join(row, " | "))
			c.buf.WriteByte('\n')
		}
	}
	c.buf.WriteByte('\n')
}

func (c *converter) handleDefinitionTerm(n *html.Node) {
	c.ensureDoubleNewline()
	if c.mode == modeMarkdown {
		c.buf.WriteString("**")
		c.walkChildren(n)
		c.buf.WriteString("**")
	} else {
		c.walkChildren(n)
	}
	c.buf.WriteByte('\n')
}

func (c *converter) handleDefinitionDesc(n *html.Node) {
	c.ensureNewline()
	if c.mode == modeMarkdown {
		c.buf.WriteString(": ")
	}
	c.walkChildren(n)
	c.buf.WriteByte('\n')
}
