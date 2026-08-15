package tools

import (
	"strings"
	"testing"
)

func TestHtmlToMarkdown_Headings(t *testing.T) {
	html := `<html><body><h1>Title</h1><h2>Subtitle</h2><h3>Section</h3></body></html>`
	got := htmlToMarkdown(html)
	for _, want := range []string{"# Title", "## Subtitle", "### Section"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestHtmlToMarkdown_Paragraphs(t *testing.T) {
	html := `<html><body><p>First paragraph.</p><p>Second paragraph.</p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "First paragraph.") || !strings.Contains(got, "Second paragraph.") {
		t.Errorf("unexpected output:\n%s", got)
	}
}

func TestHtmlToMarkdown_Links(t *testing.T) {
	html := `<html><body><p>Visit <a href="https://example.com">Example</a> site.</p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "[Example](https://example.com)") {
		t.Errorf("missing link in:\n%s", got)
	}
}

func TestHtmlToMarkdown_Images(t *testing.T) {
	html := `<html><body><img alt="logo" src="logo.png"></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "![logo](logo.png)") {
		t.Errorf("missing image in:\n%s", got)
	}
}

func TestHtmlToMarkdown_BoldItalic(t *testing.T) {
	html := `<html><body><p><strong>bold</strong> and <em>italic</em></p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "**bold**") {
		t.Errorf("missing bold in:\n%s", got)
	}
	if !strings.Contains(got, "*italic*") {
		t.Errorf("missing italic in:\n%s", got)
	}
}

func TestHtmlToMarkdown_PreCode(t *testing.T) {
	html := `<html><body><pre><code class="language-go">func main() {}</code></pre></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "```go") {
		t.Errorf("missing fenced code block with language in:\n%s", got)
	}
	if !strings.Contains(got, "func main() {}") {
		t.Errorf("missing code content in:\n%s", got)
	}
	if strings.Count(got, "```") < 2 {
		t.Errorf("missing closing fence in:\n%s", got)
	}
}

func TestHtmlToMarkdown_InlineCode(t *testing.T) {
	html := `<html><body><p>Use <code>fmt.Println</code> to print.</p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "`fmt.Println`") {
		t.Errorf("missing inline code in:\n%s", got)
	}
}

func TestHtmlToMarkdown_Blockquote(t *testing.T) {
	html := `<html><body><blockquote><p>A wise quote.</p></blockquote></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "> ") {
		t.Errorf("missing blockquote prefix in:\n%s", got)
	}
	if !strings.Contains(got, "A wise quote.") {
		t.Errorf("missing quote content in:\n%s", got)
	}
}

func TestHtmlToMarkdown_UnorderedList(t *testing.T) {
	html := `<html><body><ul><li>One</li><li>Two</li><li>Three</li></ul></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- One") || !strings.Contains(got, "- Two") || !strings.Contains(got, "- Three") {
		t.Errorf("missing list items in:\n%s", got)
	}
}

func TestHtmlToMarkdown_OrderedList(t *testing.T) {
	html := `<html><body><ol><li>First</li><li>Second</li></ol></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "1. First") || !strings.Contains(got, "2. Second") {
		t.Errorf("missing ordered list items in:\n%s", got)
	}
}

func TestHtmlToMarkdown_NestedList(t *testing.T) {
	html := `<html><body><ul><li>A<ul><li>A1</li><li>A2</li></ul></li><li>B</li></ul></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- A") || !strings.Contains(got, "  - A1") || !strings.Contains(got, "  - A2") {
		t.Errorf("missing nested list in:\n%s", got)
	}
}

func TestHtmlToMarkdown_Table(t *testing.T) {
	html := `<html><body><table><tr><th>Name</th><th>Age</th></tr><tr><td>Alice</td><td>30</td></tr></table></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "| Name") || !strings.Contains(got, "| Age") {
		t.Errorf("missing table header in:\n%s", got)
	}
	if !strings.Contains(got, "| ---") {
		t.Errorf("missing table separator in:\n%s", got)
	}
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "30") {
		t.Errorf("missing table data in:\n%s", got)
	}
}

func TestHtmlToMarkdown_HorizontalRule(t *testing.T) {
	html := `<html><body><p>Above</p><hr><p>Below</p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "---") {
		t.Errorf("missing horizontal rule in:\n%s", got)
	}
}

// --- Stripping non-content elements ---

func TestHtmlToMarkdown_StripsHead(t *testing.T) {
	html := `<html><head><title>Page Title</title><meta name="desc" content="description"><link rel="stylesheet" href="style.css"><style>.foo{color:red}</style><script>var x=1;</script></head><body><p>Content</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Page Title") {
		t.Errorf("head title should not appear in output:\n%s", got)
	}
	if strings.Contains(got, "color:red") || strings.Contains(got, "var x=1") {
		t.Errorf("head CSS/JS leaked into output:\n%s", got)
	}
	if !strings.Contains(got, "Content") {
		t.Errorf("body content missing:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsScript(t *testing.T) {
	html := `<html><body><p>Hello</p><script>alert('xss')</script><p>World</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "alert") || strings.Contains(got, "xss") {
		t.Errorf("script content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsStyle(t *testing.T) {
	html := `<html><body><style>.foo { display: none; }</style><p>Text</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "display") || strings.Contains(got, ".foo") {
		t.Errorf("style content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsNoscript(t *testing.T) {
	html := `<html><body><noscript><style>.ns{color:blue}</style><p>Enable JavaScript</p></noscript><p>Real content</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Enable JavaScript") || strings.Contains(got, "color:blue") {
		t.Errorf("noscript content leaked:\n%s", got)
	}
	if !strings.Contains(got, "Real content") {
		t.Errorf("real content missing:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsSvg(t *testing.T) {
	html := `<html><body><svg xmlns="http://www.w3.org/2000/svg"><path d="M10 20 L30 40"/><text>icon</text></svg><p>Content</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "M10") || strings.Contains(got, "icon") {
		t.Errorf("SVG content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsNav(t *testing.T) {
	html := `<html><body><nav><a href="/">Home</a><a href="/about">About</a></nav><p>Article</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Home") || strings.Contains(got, "About") {
		t.Errorf("nav content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsFooter(t *testing.T) {
	html := `<html><body><p>Article</p><footer><p>Copyright 2024</p></footer></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Copyright") {
		t.Errorf("footer content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsForm(t *testing.T) {
	html := `<html><body><form><input type="text" value="name"><select><option>A</option><option>B</option></select><button>Submit</button></form><p>Content</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Submit") || strings.Contains(got, "name") {
		t.Errorf("form content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsIframe(t *testing.T) {
	html := `<html><body><iframe src="https://ads.example.com">Fallback</iframe><p>Content</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Fallback") || strings.Contains(got, "ads.example") {
		t.Errorf("iframe content leaked:\n%s", got)
	}
}

func TestHtmlToMarkdown_StripsTemplate(t *testing.T) {
	html := `<html><body><template><div>Template content</div></template><p>Visible</p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "Template content") {
		t.Errorf("template content leaked:\n%s", got)
	}
}

// --- Entity handling ---

func TestHtmlToMarkdown_Entities(t *testing.T) {
	html := `<html><body><p>A &amp; B &lt; C &gt; D &quot;E&quot; &#39;F&#39;</p></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, `A & B < C > D "E" 'F'`) {
		t.Errorf("entities not decoded properly:\n%s", got)
	}
}

// --- Whitespace handling ---

func TestHtmlToMarkdown_WhitespaceCollapse(t *testing.T) {
	html := `<html><body><p>  lots   of    spaces  </p></body></html>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "  ") {
		t.Errorf("whitespace not collapsed:\n%s", got)
	}
}

func TestHtmlToMarkdown_PreservesPreWhitespace(t *testing.T) {
	html := `<html><body><pre>  line1
  line2
  line3</pre></body></html>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "  line1\n  line2\n  line3") {
		t.Errorf("pre whitespace not preserved:\n%s", got)
	}
}

// --- Malformed HTML ---

func TestHtmlToMarkdown_MalformedHTML(t *testing.T) {
	html := `<p>Unclosed paragraph<div>Nested <b>bold</div></p><p>OK</p>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Unclosed paragraph") || !strings.Contains(got, "bold") || !strings.Contains(got, "OK") {
		t.Errorf("malformed HTML not handled:\n%s", got)
	}
}

// --- Text mode ---

func TestHtmlToText_NoMarkdownFormatting(t *testing.T) {
	html := `<html><body><h1>Title</h1><p>Text with <strong>bold</strong> and <a href="https://example.com">link</a>.</p></body></html>`
	got := htmlToText(html)
	if strings.Contains(got, "#") || strings.Contains(got, "**") || strings.Contains(got, "[link]") {
		t.Errorf("markdown formatting in text mode:\n%s", got)
	}
	if !strings.Contains(got, "Title") || !strings.Contains(got, "bold") || !strings.Contains(got, "link") {
		t.Errorf("content missing in text mode:\n%s", got)
	}
}

func TestHtmlToText_StripsHeader(t *testing.T) {
	html := `<html><body><header><h1>Site Name</h1></header><p>Article</p></body></html>`
	got := htmlToText(html)
	if strings.Contains(got, "Site Name") {
		t.Errorf("header content should be stripped in text mode:\n%s", got)
	}
	if !strings.Contains(got, "Article") {
		t.Errorf("body content missing:\n%s", got)
	}
}

func TestHtmlToText_StripsAside(t *testing.T) {
	html := `<html><body><aside><p>Sidebar</p></aside><p>Main content</p></body></html>`
	got := htmlToText(html)
	if strings.Contains(got, "Sidebar") {
		t.Errorf("aside content should be stripped in text mode:\n%s", got)
	}
}

// --- Realistic SPA page ---

func TestHtmlToMarkdown_SPAPage(t *testing.T) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>My App</title>
<link rel="stylesheet" href="/static/css/main.abc123.css">
<style>
body { margin: 0; font-family: sans-serif; }
.header { background: #333; color: white; }
.container { max-width: 800px; margin: auto; }
@media (max-width: 768px) { .container { padding: 10px; } }
</style>
<script>
window.__INITIAL_STATE__ = {"user":null,"theme":"dark"};
</script>
</head>
<body>
<noscript>You need to enable JavaScript to run this app.</noscript>
<nav>
<a href="/">Home</a>
<a href="/about">About</a>
<a href="/contact">Contact</a>
</nav>
<div id="root">
<h1>Welcome to My App</h1>
<p>This is the main content of the page.</p>
<ul>
<li>Feature 1</li>
<li>Feature 2</li>
</ul>
</div>
<footer>
<p>&copy; 2024 My Company. All rights reserved.</p>
</footer>
<script src="/static/js/main.def456.js"></script>
<script>
(function() { console.log("analytics loaded"); })();
</script>
</body>
</html>`

	got := htmlToMarkdown(html)

	// Should NOT contain CSS/JS artifacts
	for _, bad := range []string{
		"margin: 0", "sans-serif", "background: #333",
		"__INITIAL_STATE__", "theme", "dark",
		"analytics loaded", "console.log",
		"main.abc123.css", "main.def456.js",
		"enable JavaScript",
		"viewport",     // meta from head
		"All rights",   // footer
		"Home", "About", "Contact", // nav
	} {
		if strings.Contains(got, bad) {
			t.Errorf("non-content %q leaked into output:\n%s", bad, got)
		}
	}

	// Should contain actual content
	for _, good := range []string{
		"# Welcome to My App",
		"main content of the page",
		"- Feature 1",
		"- Feature 2",
	} {
		if !strings.Contains(got, good) {
			t.Errorf("expected content %q missing from output:\n%s", good, got)
		}
	}
}
