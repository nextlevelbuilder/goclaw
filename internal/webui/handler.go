package webui

import (
	"io/fs"
	"net/http"
	"strings"
)

// apiPrefixes are URL prefixes reserved for backend APIs.
// Requests matching these are never served by the SPA handler.
var apiPrefixes = []string{"/v1/", "/ws", "/health", "/mcp/"}

// Handler returns an http.Handler that serves the embedded SPA.
// Returns nil if no assets are embedded (built without embedui tag).
func Handler() http.Handler {
	fsys := Assets()
	if fsys == nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(fsys))
	return &spaHandler{fs: fsys, fileServer: fileServer}
}

type spaHandler struct {
	fs         fs.FS
	fileServer http.Handler
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
		}
		h.fileServer.ServeHTTP(w, r)
		return
	}

	// SPA fallback: serve index.html for any unmatched route.
	// This handles client-side routing (React Router).
	r.URL.Path = "/"
	h.fileServer.ServeHTTP(w, r)
}
