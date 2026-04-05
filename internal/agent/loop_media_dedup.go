package agent

import (
	"path/filepath"
	"strings"
)

// deduplicateMediaSuffix filters ContentSuffix lines, removing any whose file
// basename already appears in the main content. This prevents duplicate file
// references when the agent's own text already includes the same media.
//
// ContentSuffix format (from mediaToMarkdown):
//
//	\n\n![image](/v1/files/path/to/file.png)
//	[filename.md](/v1/files/path/to/filename.md)
func deduplicateMediaSuffix(content, suffix string) string {
	lines := strings.Split(suffix, "\n")
	var kept []string
	for _, line := range lines {
		if line == "" {
			kept = append(kept, line)
			continue
		}
		// Extract basename from the URL in markdown link/image: ](url) or ](url)
		base := extractLinkBasename(line)
		if base != "" && containsMediaRef(content, base) {
			continue // already referenced in content
		}
		kept = append(kept, line)
	}
	result := strings.Join(kept, "\n")
	// If only empty lines remain, return empty string.
	if strings.TrimSpace(result) == "" {
		return ""
	}
	return result
}

// containsMediaRef checks if content contains a URL reference to a file by basename.
// Requires URL context (slash before, paren or query after) to avoid false positives
// matching prose text like "I saved output.png".
func containsMediaRef(content, basename string) bool {
	return strings.Contains(content, "/"+basename+")") ||
		strings.Contains(content, "/"+basename+"?") ||
		strings.Contains(content, "/"+basename+"\n") ||
		strings.HasSuffix(content, "/"+basename)
}

// extractLinkBasename extracts the filename (basename) from a markdown link or
// image line. Handles: ![alt](url), [text](url).
func extractLinkBasename(line string) string {
	// Find the URL between ]( and )
	idx := strings.Index(line, "](")
	if idx < 0 {
		return ""
	}
	urlStart := idx + 2
	urlEnd := strings.LastIndex(line, ")")
	if urlEnd <= urlStart {
		return ""
	}
	url := line[urlStart:urlEnd]
	// Strip query params before taking basename.
	if qIdx := strings.Index(url, "?"); qIdx > 0 {
		url = url[:qIdx]
	}
	return filepath.Base(url)
}
