package store

import (
	"path/filepath"
	"strings"
)

const SkillMarkdownFilename = "SKILL.md"

// SkillBaseDir normalizes a persisted skill file_path into the skill directory.
// Older import paths may point directly to SKILL.md; managed uploads usually
// point to the version directory.
func SkillBaseDir(filePath string) string {
	if filePath == "" {
		return ""
	}
	cleaned := filepath.Clean(filePath)
	if cleaned == "." {
		return ""
	}
	if strings.EqualFold(filepath.Base(cleaned), SkillMarkdownFilename) {
		return filepath.Dir(cleaned)
	}
	return cleaned
}

func SkillMarkdownPath(filePath string) string {
	baseDir := SkillBaseDir(filePath)
	if baseDir == "" {
		return ""
	}
	return filepath.Join(baseDir, SkillMarkdownFilename)
}

// SkillSlugDir returns the directory that contains version subdirectories.
func SkillSlugDir(filePath string) string {
	baseDir := SkillBaseDir(filePath)
	if baseDir == "" {
		return ""
	}
	return filepath.Dir(baseDir)
}
