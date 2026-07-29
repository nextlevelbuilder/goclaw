package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
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
	if err := tools.ValidateRegularFileForRead(cleaned); err != nil {
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

// confineToAnyRoot accepts an absolute mediaPath if it is contained by any
// allowed root. Relative paths are intentionally anchored to the first root,
// which is always the active tool workspace; extra roots authorize explicit
// paths but never change the meaning of an ambiguous relative path.
func confineToAnyRoot(mediaPath string, roots []string) (string, bool) {
	if len(roots) == 0 {
		return "", false
	}
	if !filepath.IsAbs(mediaPath) {
		return confineToWorkspace(mediaPath, roots[0])
	}
	for i, root := range roots {
		if root == "" {
			continue
		}
		// The active workspace (index 0) may itself be reached through an
		// operator-managed symlink. Extra read roots are authorization
		// boundaries and must be real directories, not partner-controlled
		// symlink aliases to arbitrary locations.
		if i > 0 {
			if info, err := os.Lstat(root); err == nil &&
				(info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
				continue
			}
		}
		if cleaned, ok := confineToWorkspace(mediaPath, root); ok {
			return cleaned, true
		}
	}
	return "", false
}

// mediaEgressRoots returns every read-authorized root that may supply outbound
// media. The active workspace stays first to preserve relative path semantics.
func (l *Loop) mediaEgressRoots(ctx context.Context) []string {
	tenantAllowedPaths := tools.TenantAllowedPathsFromCtx(ctx)
	candidates := make([]string, 0, 3+len(tenantAllowedPaths))
	candidates = append(candidates,
		tools.ToolWorkspaceFromCtx(ctx),
		tools.ToolTeamWorkspaceFromCtx(ctx),
		tools.ToolTeamRootFromCtx(ctx),
	)
	candidates = append(candidates, tenantAllowedPaths...)

	seen := make(map[string]struct{}, len(candidates))
	roots := make([]string, 0, len(candidates))
	for _, root := range candidates {
		if root == "" {
			continue
		}
		cleaned := filepath.Clean(root)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		roots = append(roots, cleaned)
	}
	return roots
}

// extractMediaFromContent scans text for MEDIA:<path> tokens the LLM may echo
// in its final response (e.g. when a tool returned the MEDIA: prefix as plain
// text instead of setting Result.Media). The first root is the active workspace
// used for relative paths; later roots authorize explicit absolute paths.
// Called before sanitize strips the tokens so the attachments are delivered.
//
// Security: only paths accepted by confineToAnyRoot are emitted.
func extractMediaFromContent(content string, roots []string) []MediaResult {
	if !strings.Contains(content, "MEDIA:") || len(roots) == 0 {
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
		cleaned, ok := confineToAnyRoot(path, roots)
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
