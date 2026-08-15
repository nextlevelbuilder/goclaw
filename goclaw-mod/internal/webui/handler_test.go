package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestHandlerInjectsBrandingMetadataIntoIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html><html><head><title>GoClaw</title><meta name="description" content=""><meta property="og:title" content="GoClaw"><link rel="icon" href="/favicon.ico"><meta name="theme-color" content="#000000"></head><body><div id="root"></div></body></html>`)},
	}
	cfg := config.Default()
	cfg.Branding = config.BrandingConfig{
		AppName:           "Acme Agents",
		MetaTitle:         "Acme Agents Console",
		MetaDescription:   "Private agent dashboard",
		MetaKeywords:      "agents, automation",
		LogoURL:           "/branding-assets/logo.png",
		FaviconURL:        "/branding-assets/favicon.ico",
		AppleTouchIconURL: "/branding-assets/apple.png",
		OGTitle:           "Acme Social Title",
		OGDescription:     "Acme social description",
		OGImageURL:        "/branding-assets/og.webp",
		ThemeColor:        "#123456",
	}

	h := NewHandlerForFS(fsys, cfg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<title>Acme Agents Console</title>`,
		`<script id="goclaw-branding" type="application/json">`,
		`"app_name":"Acme Agents"`,
		`"logo_url":"/branding-assets/logo.png"`,
		`<meta name="application-name" content="Acme Agents">`,
		`<meta name="description" content="Private agent dashboard">`,
		`<meta name="keywords" content="agents, automation">`,
		`<meta property="og:title" content="Acme Social Title">`,
		`<meta property="og:description" content="Acme social description">`,
		`<meta property="og:image" content="/branding-assets/og.webp">`,
		`<link rel="icon" href="/branding-assets/favicon.ico">`,
		`<link rel="apple-touch-icon" href="/branding-assets/apple.png">`,
		`<meta name="theme-color" content="#123456">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index response missing %q in:\n%s", want, body)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestHandlerKeepsHashedAssetsImmutable(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte(`<!doctype html><title>GoClaw</title>`)},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte(`console.log("ok")`)},
	}

	h := NewHandlerForFS(fsys, config.Default())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if strings.Contains(rec.Body.String(), "Acme") {
		t.Fatalf("asset response should not be rewritten: %q", rec.Body.String())
	}
}

var _ fs.FS = fstest.MapFS{}
