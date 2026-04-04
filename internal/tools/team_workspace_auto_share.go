package tools

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxAutoShareFileSize caps individual file copies to prevent large file transfers.
const maxAutoShareFileSize = 10 * 1024 * 1024 // 10 MB

// knownFileExtensions lists extensions that are likely intentional file references.
// Excludes .env (secrets risk) and binary formats.
var knownFileExtensions = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".csv": true, ".yaml": true, ".yml": true,
	".py": true, ".go": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".html": true, ".css": true, ".sql": true, ".xml": true, ".toml": true,
	".cfg": true, ".ini": true, ".log": true, ".pdf": true,
}

// filePathPatterns matches file references in task descriptions.
// Order matters — more specific patterns first.
var filePathPatterns = []*regexp.Regexp{
	// path="file.md" or path='file.md' (tool call references)
	regexp.MustCompile(`path\s*=\s*["']([^"']+)["']`),
	// "file.md" or 'file.md' (quoted references)
	regexp.MustCompile(`["']([a-zA-Z0-9_./-]+\.[a-zA-Z0-9]+)["']`),
	// [text](file.md) (markdown links — exclude URLs)
	regexp.MustCompile(`\]\(([a-zA-Z0-9_./-]+\.[a-zA-Z0-9]+)\)`),
	// backtick references: `file.md`
	regexp.MustCompile("`([a-zA-Z0-9_./-]+\\.[a-zA-Z0-9]+)`"),
}

// extractFilePaths finds file path references in text.
// Returns deduplicated list of relative paths with known extensions.
func extractFilePaths(text string) []string {
	seen := make(map[string]bool)
	var paths []string

	for _, pat := range filePathPatterns {
		for _, match := range pat.FindAllStringSubmatch(text, -1) {
			if len(match) < 2 {
				continue
			}
			p := match[1]
			if !isValidFilePath(p) {
				continue
			}
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// isValidFilePath checks if a string looks like an intentional file reference.
func isValidFilePath(p string) bool {
	// Skip URLs
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "ftp://") {
		return false
	}
	// Skip absolute paths (security)
	if filepath.IsAbs(p) {
		return false
	}
	// Skip path traversal
	if strings.Contains(p, "..") {
		return false
	}
	// Must have a known extension
	ext := strings.ToLower(filepath.Ext(p))
	return knownFileExtensions[ext]
}

// autoShareFiles scans description for file paths, copies files from personal
// workspace to team workspace if they exist in personal but not in team.
// Fire-and-forget: logs errors, never fails the caller.
func autoShareFiles(description, personalWs, teamWs string) int {
	if personalWs == "" || teamWs == "" || personalWs == teamWs {
		return 0
	}

	paths := extractFilePaths(description)
	if len(paths) == 0 {
		return 0
	}

	copied := 0
	for _, relPath := range paths {
		srcPath := filepath.Join(personalWs, relPath)
		dstPath := filepath.Join(teamWs, relPath)

		// Check source exists in personal workspace (Lstat to reject symlinks).
		srcInfo, err := os.Lstat(srcPath)
		if err != nil || srcInfo.IsDir() || srcInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		// Skip files exceeding size cap.
		if srcInfo.Size() > maxAutoShareFileSize {
			slog.Debug("auto_share: skipping large file", "file", relPath, "size", srcInfo.Size())
			continue
		}

		// Skip if already in team workspace (same content assumed).
		if _, err := os.Stat(dstPath); err == nil {
			continue
		}

		// Create parent dirs and copy.
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			slog.Warn("auto_share: mkdir failed", "dst", dstPath, "error", err)
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			slog.Warn("auto_share: copy failed", "src", srcPath, "dst", dstPath, "error", err)
			continue
		}

		copied++
		slog.Info("auto_share: copied file to team workspace", "file", relPath, "src", srcPath, "dst", dstPath)
	}
	return copied
}

// copyFile is defined in workspace_media.go — reused here.
