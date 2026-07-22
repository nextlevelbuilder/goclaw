package webui

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// apiPrefixes are URL prefixes reserved for backend APIs.
// Requests matching these are never served by the SPA handler.
var apiPrefixes = []string{"/v1/", "/ws", "/health", "/api/mcp/"}

// Handler returns an http.Handler that serves the embedded SPA.
// Returns nil if no assets are embedded (built without embedui tag).
func Handler(cfg *config.Config) http.Handler {
	fsys := Assets()
	if fsys == nil {
		return nil
	}
	return NewHandlerForFS(fsys, cfg)
}

// NewHandlerForFS returns a SPA handler for the provided filesystem.
// It is exported for tests so embedded-asset behavior can be verified without
// requiring an embedui build.
func NewHandlerForFS(fsys fs.FS, cfg *config.Config) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return &spaHandler{fs: fsys, fileServer: fileServer, cfg: cfg}
}

type spaHandler struct {
	fs         fs.FS
	fileServer http.Handler
	cfg        *config.Config
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Never intercept API routes.
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
	}

	// Try to serve the file directly.
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Check if file exists in the embedded FS.
	if _, err := fs.Stat(h.fs, path); err == nil {
		// Static assets: set long cache for /assets/* (Vite hashed filenames).
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			setIndexCacheHeaders(w)
		}
		if path == "index.html" {
			h.serveIndex(w, r)
			return
		}
		h.fileServer.ServeHTTP(w, r)
		return
	}

	// SPA fallback: serve index.html for any unmatched route.
	// This handles client-side routing (React Router).
	h.serveIndex(w, r)
}

func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	setIndexCacheHeaders(w)
	data, err := fs.ReadFile(h.fs, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data = injectBrandingMetadata(data, h.cfg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func setIndexCacheHeaders(w http.ResponseWriter) {
	// index.html and SPA fallback routes must never be cached so the browser
	// always fetches the latest Vite asset manifest after an embedded UI rebuild.
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
}

func injectBrandingMetadata(index []byte, cfg *config.Config) []byte {
	if cfg == nil {
		return index
	}
	branding := cfg.BrandingSnapshot()
	if !branding.HasValues() {
		return index
	}

	htmlDoc := string(index)
	title := firstNonEmpty(branding.MetaTitle, branding.AppName)
	ogTitle := firstNonEmpty(branding.OGTitle, title)
	ogDescription := firstNonEmpty(branding.OGDescription, branding.MetaDescription)

	htmlDoc = replaceTitle(htmlDoc, title)
	tags := []string{
		metaName("application-name", branding.AppName),
		metaName("description", branding.MetaDescription),
		metaName("keywords", branding.MetaKeywords),
		metaProperty("og:title", ogTitle),
		metaProperty("og:description", ogDescription),
		metaProperty("og:image", branding.OGImageURL),
		linkRel("icon", branding.FaviconURL),
		linkRel("apple-touch-icon", branding.AppleTouchIconURL),
		metaName("theme-color", branding.ThemeColor),
	}
	for _, tag := range tags {
		if tag != "" {
			htmlDoc = insertOrReplaceHeadTag(htmlDoc, tag)
		}
	}
	if script := runtimeBrandingScript(branding); script != "" {
		htmlDoc = insertOrReplaceHeadTag(htmlDoc, script)
	}
	return []byte(htmlDoc)
}

func runtimeBrandingScript(branding config.BrandingConfig) string {
	payload := map[string]string{}
	for key, value := range map[string]string{
		"app_name":       branding.AppName,
		"app_short_name": branding.AppShortName,
		"logo_url":       branding.LogoURL,
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			payload[key] = trimmed
		}
	}
	if len(payload) == 0 {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return `<script id="goclaw-branding" type="application/json">` + string(data) + `</script>`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func replaceTitle(doc, title string) string {
	if strings.TrimSpace(title) == "" {
		return doc
	}
	re := regexp.MustCompile(`(?is)<title[^>]*>.*?</title>`)
	replacement := "<title>" + html.EscapeString(title) + "</title>"
	if re.MatchString(doc) {
		return re.ReplaceAllStringFunc(doc, func(string) string { return replacement })
	}
	return insertBeforeHeadClose(doc, replacement)
}

func metaName(name, content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return fmt.Sprintf(`<meta name="%s" content="%s">`, html.EscapeString(name), html.EscapeString(strings.TrimSpace(content)))
}

func metaProperty(property, content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return fmt.Sprintf(`<meta property="%s" content="%s">`, html.EscapeString(property), html.EscapeString(strings.TrimSpace(content)))
}

func linkRel(rel, href string) string {
	if strings.TrimSpace(href) == "" {
		return ""
	}
	return fmt.Sprintf(`<link rel="%s" href="%s">`, html.EscapeString(rel), html.EscapeString(strings.TrimSpace(href)))
}

func insertOrReplaceHeadTag(doc, tag string) string {
	selector := headTagSelector(tag)
	if selector == "" {
		return insertBeforeHeadClose(doc, tag)
	}
	re := regexp.MustCompile(selector)
	if re.MatchString(doc) {
		return re.ReplaceAllStringFunc(doc, func(string) string { return tag })
	}
	return insertBeforeHeadClose(doc, tag)
}

func headTagSelector(tag string) string {
	for _, attr := range []string{"name", "property", "rel"} {
		prefix := attr + `="`
		start := strings.Index(tag, prefix)
		if start < 0 {
			continue
		}
		start += len(prefix)
		end := strings.Index(tag[start:], `"`)
		if end < 0 {
			return ""
		}
		value := regexp.QuoteMeta(tag[start : start+end])
		switch attr {
		case "name":
			return `(?is)<meta\b[^>]*\bname=["']` + value + `["'][^>]*>`
		case "property":
			return `(?is)<meta\b[^>]*\bproperty=["']` + value + `["'][^>]*>`
		case "rel":
			return `(?is)<link\b[^>]*\brel=["']` + value + `["'][^>]*>`
		}
	}
	return ""
}

func insertBeforeHeadClose(doc, tag string) string {
	re := regexp.MustCompile(`(?i)</head>`)
	if re.MatchString(doc) {
		return re.ReplaceAllString(doc, tag+"\n</head>")
	}
	return tag + "\n" + doc
}
