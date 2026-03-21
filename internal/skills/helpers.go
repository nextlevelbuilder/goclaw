package skills

import (
	"path/filepath"
	"regexp"
	"strings"
)

// SlugRegexp validates skill slugs: lowercase alphanumeric with hyphens, no leading/trailing hyphen.
var SlugRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

// ParseSkillFrontmatter extracts name, description, and slug from SKILL.md YAML frontmatter.
// Also returns the full parsed frontmatter as a map for DB storage.
func ParseSkillFrontmatter(content string) (name, description, slug string, allFields map[string]string) {
	allFields = make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return "", "", "", allFields
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", "", "", allFields
	}
	fm := content[3 : 3+end]
	allFields = parseSimpleYAML(fm)
	name = allFields["name"]
	description = allFields["description"]
	slug = allFields["slug"]
	return
}

// Slugify converts a skill name into a valid slug (lowercase, alphanumeric + hyphens).
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "skill"
	}
	return s
}

// IsSystemArtifact returns true for OS-generated junk that should be skipped
// during file extraction and listing (e.g. __MACOSX, .DS_Store, Thumbs.db).
func IsSystemArtifact(name string) bool {
	base := filepath.Base(name)
	// macOS resource fork / metadata folders and files
	if base == "__MACOSX" || strings.HasPrefix(base, "._") {
		return true
	}
	// Check if any path component is __MACOSX
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == "__MACOSX" {
			return true
		}
	}
	// Common OS junk files
	switch base {
	case ".DS_Store", "Thumbs.db", "desktop.ini", ".Spotlight-V100", ".Trashes", ".fseventsd":
		return true
	}
	return false
}
