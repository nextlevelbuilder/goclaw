package nodehost

import (
	"path/filepath"
	"strings"
)

var windowsExecutableSuffixes = []string{".exe", ".cmd", ".bat", ".com"}

// stripWindowsExecutableSuffix removes Windows executable suffixes from a binary name.
func stripWindowsExecutableSuffix(value string) string {
	lower := strings.ToLower(value)
	for _, suffix := range windowsExecutableSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return value[:len(value)-len(suffix)]
		}
	}
	return value
}

// BasenameLower extracts the basename from a token, using both posix and windows
// path separators, then lowercases it. Picks the shorter result to handle
// cross-platform paths.
func BasenameLower(token string) string {
	posix := filepath.Base(token)
	// Also try Windows-style separator for cross-platform compat.
	win := token
	if idx := strings.LastIndex(token, "\\"); idx >= 0 {
		win = token[idx+1:]
	}
	base := posix
	if len(win) < len(posix) {
		base = win
	}
	return strings.ToLower(strings.TrimSpace(base))
}

// NormalizeExecutableToken extracts the lowercase basename of a command token,
// stripping path components and Windows executable suffixes.
func NormalizeExecutableToken(token string) string {
	return stripWindowsExecutableSuffix(BasenameLower(token))
}
