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

// deduplicateMedia removes duplicate media results by path, keeping the first occurrence.
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

// extractWorkspaceMedia scans content for workspace file paths that look like media files.
// This is a fallback for agents that create media via exec/browser tools without MEDIA: prefix.
// Matches patterns like: /Users/.../workspace/.../filename.(png|jpg|mp4|...)
//
// Skips paths inside code blocks (```...```) to avoid false positives.
func extractWorkspaceMedia(content string) []MediaResult {
	// Common media extensions
	mediaExts := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".gif": "image/gif", ".webp": "image/webp",
		".mp4": "video/mp4", ".webm": "video/webm",
		".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav",
	}

	// Strip code blocks to avoid false positives (paths in examples/docs)
	cleaned := stripCodeBlocks(content)

	var results []MediaResult
	seen := make(map[string]bool)

	lines := strings.Split(cleaned, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip if already has MEDIA: prefix (handled by parseMediaResult)
		if strings.HasPrefix(trimmed, "MEDIA:") {
			continue
		}
		// Skip lines that look like shell commands or code
		if strings.HasPrefix(trimmed, "$") || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Look for paths in backticks or as plain text
		for _, word := range strings.Fields(line) {
			// Clean up markdown formatting
			path := strings.Trim(word, "`*_[]()\"'<>")
			// Must be absolute path with workspace marker
			if !strings.HasPrefix(path, "/") {
				continue
			}
			if !strings.Contains(path, "/workspace/") && !strings.Contains(path, "/.goclaw/") {
				continue
			}
			// Skip if already found (dedup within this function)
			if seen[path] {
				continue
			}
			ext := strings.ToLower(filepath.Ext(path))
			if mime, ok := mediaExts[ext]; ok {
				// Verify file exists before adding
				if info, err := os.Stat(path); err == nil && info.Size() > 0 {
					seen[path] = true
					results = append(results, MediaResult{Path: path, ContentType: mime})
				}
			}
		}
	}
	return results
}

// stripCodeBlocks removes fenced code blocks (```...```) from content.
func stripCodeBlocks(content string) string {
	var result strings.Builder
	inCodeBlock := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if !inCodeBlock {
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}
	return result.String()
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
