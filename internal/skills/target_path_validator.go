package skills

import (
	"fmt"
	"path"
	"strings"
)

// ValidateSkillTargetPath validates a relative path inside a managed skill root.
// Set allowSkillMD when the caller is explicitly editing the primary SKILL.md.
func ValidateSkillTargetPath(rawPath string, allowSkillMD bool) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("invalid file path %q: empty path", rawPath)
	}
	if strings.ContainsRune(rawPath, 0x00) {
		return "", fmt.Errorf("invalid file path %q: null byte", rawPath)
	}
	if len(rawPath) >= 2 && rawPath[1] == ':' {
		return "", fmt.Errorf("invalid file path %q: windows drive paths are not allowed", rawPath)
	}
	normalized := strings.ReplaceAll(rawPath, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("invalid file path %q: absolute paths are not allowed", rawPath)
	}
	for part := range strings.SplitSeq(normalized, "/") {
		switch part {
		case "..":
			return "", fmt.Errorf("invalid file path %q: parent traversal is not allowed", rawPath)
		case ".git":
			return "", fmt.Errorf("invalid file path %q: system artifact paths are not allowed", rawPath)
		}
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("invalid file path %q: hidden files are not allowed", rawPath)
		}
	}
	cleanPath := path.Clean(normalized)
	if cleanPath == "." || strings.HasPrefix(cleanPath, "../") || cleanPath == ".." || strings.HasPrefix(cleanPath, "/") {
		return "", fmt.Errorf("invalid file path %q: path escapes skill root", rawPath)
	}
	if strings.EqualFold(cleanPath, "SKILL.md") {
		if allowSkillMD {
			return "SKILL.md", nil
		}
		return "", fmt.Errorf("invalid file path %q: SKILL.md must be provided via content or find/replace", rawPath)
	}
	if IsSystemArtifact(cleanPath) {
		return "", fmt.Errorf("invalid file path %q: system artifact paths are not allowed", rawPath)
	}
	return cleanPath, nil
}
