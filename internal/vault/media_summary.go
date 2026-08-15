package vault

import (
	"path/filepath"
	"regexp"
	"strings"
)

// mediaSummaryMaxLen caps the synthesized summary length so embeddings
// aren't dominated by filename text. Value chosen to accommodate a label
// + 4-6 tokens + parent-dir context with comfortable headroom.
const mediaSummaryMaxLen = 200

var (
	// Split camelCase tokens at lowercase→uppercase boundaries.
	camelRe = regexp.MustCompile(`([a-z])([A-Z])`)
	// Meaningless tokens dropped from semantic token output.
	stopTokens = map[string]bool{
		"img": true, "dsc": true, "dscn": true, "image": true,
		"photo": true, "video": true, "audio": true, "file": true,
	}
	pureDigits = regexp.MustCompile(`^\d+$`)
)

// SynthesizeMediaSummary builds a deterministic pseudo-summary for media and
// document files. Pure function: no LLM, no file I/O, no global state.
//
// Used by the enrich worker to populate `vault_documents.summary` for
// media/document rows so vector search can find them via filename semantics
// + parent folder context instead of an empty-string signal.
//
// Examples:
//
//	SynthesizeMediaSummary("photos/vacation/cat-on-beach.png", "image/png")
//	  → "Image — cat on beach (from photos/vacation)"
//	SynthesizeMediaSummary("docs/specs/api-v2.pdf", "application/pdf")
//	  → "Document — api v2 (from docs/specs)"
//	SynthesizeMediaSummary("photos/IMG_20240101_001.jpg", "image/jpeg")
//	  → "Image file IMG_20240101_001 (from photos)"
func SynthesizeMediaSummary(relPath, mimeType string) string {
	base := filepath.Base(relPath)
	nameNoExt := strings.TrimSuffix(base, filepath.Ext(base))
	parent := parentSegments(relPath, 2)
	label := mimeTypeLabel(mimeType, filepath.Ext(relPath))

	tokens := splitSemanticTokens(nameNoExt)
	var out string
	if len(tokens) > 0 {
		out = label + " — " + strings.Join(tokens, " ") + " (from " + parent + ")"
	} else {
		out = label + " file " + nameNoExt + " (from " + parent + ")"
	}
	if len(out) > mediaSummaryMaxLen {
		out = out[:mediaSummaryMaxLen]
	}
	return out
}

// mimeTypeLabel returns a human-readable category label for a mime type.
// Falls back to the extension whitelist when mime is empty or unknown.
func mimeTypeLabel(mime, ext string) string {
	mime = strings.ToLower(mime)
	ext = strings.ToLower(ext)
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "Image"
	case strings.HasPrefix(mime, "video/"):
		return "Video"
	case strings.HasPrefix(mime, "audio/"):
		return "Audio"
	case strings.HasPrefix(mime, "application/pdf"),
		strings.Contains(mime, "officedocument"),
		strings.Contains(mime, "msword"),
		strings.Contains(mime, "ms-excel"),
		strings.Contains(mime, "ms-powerpoint"):
		return "Document"
	}
	// Fall back to extension whitelist (shared with safe_walk / InferDocType).
	if dt, ok := extensionDocType[ext]; ok {
		switch dt {
		case "media":
			switch ext {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp":
				return "Image"
			case ".mp4", ".webm", ".mov", ".avi", ".mkv":
				return "Video"
			case ".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a":
				return "Audio"
			}
		case "document":
			return "Document"
		}
	}
	return "File"
}

// parentSegments returns up to the last n parent-dir segments joined by "/".
// Returns "." when the file lives at the repo root.
func parentSegments(relPath string, n int) string {
	dir := filepath.ToSlash(filepath.Dir(relPath))
	if dir == "." || dir == "/" {
		return "."
	}
	segs := strings.Split(dir, "/")
	if len(segs) > n {
		segs = segs[len(segs)-n:]
	}
	return strings.Join(segs, "/")
}

// splitSemanticTokens splits a filename stem into lowercased semantic tokens,
// dropping stop words and pure-digit runs. Returns nil if nothing meaningful
// remains (caller falls back to raw basename for stability).
func splitSemanticTokens(name string) []string {
	// Pre-split camelCase: "apiV2" → "api V2" (but "v2" has no lower/upper
	// boundary inside, so that case is handled by dash splitting below).
	name = camelRe.ReplaceAllString(name, "$1 $2")
	raw := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if tok == "" {
			continue
		}
		if stopTokens[tok] {
			continue
		}
		if pureDigits.MatchString(tok) {
			continue
		}
		out = append(out, tok)
	}
	return out
}
