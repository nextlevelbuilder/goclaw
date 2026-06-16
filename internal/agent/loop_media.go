package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// parseMediaResult extracts a MediaResult from a tool result string containing "MEDIA:" prefix.
// Handles formats: "MEDIA:/path/to/file" and "[[audio_as_voice]]\nMEDIA:/path/to/file".
// Returns nil if no MEDIA: prefix is found.
//
// IMPORTANT: Only matches "MEDIA:" at the start of the (trimmed) string to avoid false
// positives when tool output contains "MEDIA:" in arbitrary text (e.g. a web page
// mentioning a commit message like "return MEDIA: path from screenshot").
func parseMediaResult(toolOutput string) *MediaResult {
	s := toolOutput
	asVoice := false

	// Check for [[audio_as_voice]] tag (TTS voice messages)
	if strings.Contains(s, "[[audio_as_voice]]") {
		asVoice = true
		s = strings.ReplaceAll(s, "[[audio_as_voice]]", "")
	}

	s = strings.TrimSpace(s)

	// Only match MEDIA: at the beginning of the string.
	if !strings.HasPrefix(s, "MEDIA:") {
		return nil
	}
	path := strings.TrimSpace(s[6:])
	if path == "" {
		return nil
	}
	// Take only the first line (in case there's trailing text)
	if nl := strings.IndexByte(path, '\n'); nl >= 0 {
		path = strings.TrimSpace(path[:nl])
	}

	return &MediaResult{
		Path:        path,
		ContentType: mimeFromExt(filepath.Ext(path)),
		AsVoice:     asVoice,
	}
}

// confineToWorkspace validates that mediaPath resolves to a regular file located
// inside workspace, then returns the cleaned path. It is the single source of
// truth for the media path-containment boundary, shared by the two feeders of
// MediaResult.Path: extractMediaFromContent (LLM-echoed tokens) and the
// parseMediaResult sink in processToolResult (tool MEDIA: output). Constraining
// at this boundary protects every outbound channel at once — a path that escapes
// the workspace never reaches a channel's file-upload egress.
//
// Containment applies, in order:
//   - relative paths are resolved against the workspace root;
//   - Lstat (not Stat) rejects a symlink at the leaf outright;
//   - EvalSymlinks resolves ancestor symlinks before the Rel check, so a
//     "<ws>/<symlink-dir>/secret" escape via a dir symlink pointing outside the
//     workspace is caught (a purely lexical Rel check would miss it).
//
// Returns the cleaned (symlink-preserving) path and true when the file is safe
// to ship, or "", false when it must be dropped. An empty workspace yields
// false: without a boundary there is nothing to validate against, and an
// unvalidatable path must never reach an external egress.
//
// NOTE: the returned path is `cleaned`, NOT the symlink-resolved path. resolved
// is used ONLY for the containment check — downstream readers (channel senders,
// history, dedup) must see the same path semantics the tool emitted, otherwise
// workspaces backed by bind-mounts / dir symlinks suffer dedup misses (observed
// in production).
func confineToWorkspace(mediaPath, workspace string) (string, bool) {
	if mediaPath == "" || workspace == "" {
		return "", false
	}
	// Resolve workspace to its real path (follows symlinks). Required because
	// macOS uses symlinks for /tmp → /private/tmp; if we only Clean the
	// workspace but EvalSymlinks the candidate path, the Rel check below would
	// spuriously fail even for legitimate files.
	wsRoot := ""
	if abs, err := filepath.Abs(workspace); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			wsRoot = filepath.Clean(resolved)
		} else {
			wsRoot = filepath.Clean(abs)
		}
	}
	if wsRoot == "" {
		return "", false
	}
	path := mediaPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(wsRoot, path)
	}
	cleaned := filepath.Clean(path)
	info, err := os.Lstat(cleaned)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(wsRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return cleaned, true
}

// confineToAnyRoot accepts mediaPath if it is contained by ANY of the allowed
// roots (each checked with the hardened confineToWorkspace). Used for the
// result.Media egress: a tool's media legitimately lives in the agent
// workspace, the team workspace, OR a tenant-allowed path — the same scopes the
// producing tools (create_*, send_file, delegate) validate against. A path
// outside every root (e.g. /etc/passwd from a prompt-injected path) is rejected,
// so the egress guard holds without dropping legitimate cross-workspace media.
func confineToAnyRoot(mediaPath string, roots []string) (string, bool) {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if cleaned, ok := confineToWorkspace(mediaPath, root); ok {
			return cleaned, true
		}
	}
	return "", false
}

// extractMediaFromContent scans text for MEDIA:<path> tokens the LLM may echo
// in its final response (e.g. when a tool returned the MEDIA: prefix as plain
// text instead of setting Result.Media). Relative paths are resolved against
// workspace. Called before sanitize strips the tokens so the attachments are
// still delivered.
//
// Security: only paths accepted by confineToWorkspace are emitted. An LLM cannot
// inject attachments pointing at /etc/passwd, a sibling tenant's workspace, or a
// hallucinated path — the extractor silently drops them.
func extractMediaFromContent(content, workspace string) []MediaResult {
	if !strings.Contains(content, "MEDIA:") || workspace == "" {
		return nil
	}
	matches := mediaPathPattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}
	results := make([]MediaResult, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		path := strings.TrimSpace(strings.TrimPrefix(m, "MEDIA:"))
		if path == "" {
			continue
		}
		// Drop markdown/JSON trailing punctuation that would otherwise stick:
		// ")", "]", "\"", "'", ",", ";", ".".
		path = strings.TrimRight(path, `)]"',;.`)
		if path == "" {
			continue
		}
		cleaned, ok := confineToWorkspace(path, workspace)
		if !ok {
			continue
		}
		if _, dup := seen[cleaned]; dup {
			continue
		}
		seen[cleaned] = struct{}{}
		results = append(results, MediaResult{
			Path:        cleaned,
			ContentType: mimeFromExt(filepath.Ext(cleaned)),
		})
	}
	return results
}

// deduplicateMedia removes duplicate media results by path, keeping the first
// occurrence. Exact-string match is the ONLY safe comparison: filepath.Abs
// normalization depends on the process CWD, which varies across deployment
// environments and was observed to drop legitimate entries in production.
// The tiny cost of an occasional aliased-path duplicate (e.g. "./x" vs "/abs/x")
// is preferable to silently eating a real attachment.
func deduplicateMedia(media []MediaResult) []MediaResult {
	if len(media) <= 1 {
		return media
	}
	seen := make(map[string]bool, len(media))
	result := make([]MediaResult, 0, len(media))
	for _, m := range media {
		if seen[m.Path] {
			continue
		}
		seen[m.Path] = true
		result = append(result, m)
	}
	return result
}

// mimeFromExt returns a MIME type for common media file extensions.
func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".txt":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".zip":
		return "application/zip"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	case ".md":
		return "text/markdown"
	default:
		return "application/octet-stream"
	}
}
