package pg

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// skillFrontmatterRe matches YAML frontmatter (--- delimited) at the start of a file.
var skillFrontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)

func (s *PGSkillStore) LoadSkill(name string) (string, bool) {
	var slug string
	var version int
	var filePath *string
	err := s.db.QueryRow(
		"SELECT slug, version, file_path FROM skills WHERE slug = $1 AND status = 'active'", name,
	).Scan(&slug, &version, &filePath)
	if err != nil {
		return "", false
	}
	info := buildSkillInfo("", "", slug, nil, version, s.baseDir, filePath)
	content, err := readSkillFile(info.Path)
	if err != nil {
		return "", false
	}
	return content, true
}

func (s *PGSkillStore) LoadForContext(allowList []string) string {
	skills := s.FilterSkills(allowList)
	if len(skills) == 0 {
		return ""
	}
	var parts []string
	for _, sk := range skills {
		content, ok := s.LoadSkill(sk.Name)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", sk.Name, content))
	}
	if len(parts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString("## Available Skills\n\n")
	for i, p := range parts {
		if i > 0 {
			result.WriteString("\n\n---\n\n")
		}
		result.WriteString(p)
	}
	return result.String()
}

func (s *PGSkillStore) BuildSummary(allowList []string) string {
	skills := s.FilterSkills(allowList)
	if len(skills) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString("<available_skills>\n")
	for _, sk := range skills {
		result.WriteString("  <skill>\n")
		result.WriteString(fmt.Sprintf("    <name>%s</name>\n", sk.Name))
		result.WriteString(fmt.Sprintf("    <description>%s</description>\n", sk.Description))
		result.WriteString(fmt.Sprintf("    <location>%s</location>\n", sk.Path))
		result.WriteString("  </skill>\n")
	}
	result.WriteString("</available_skills>")
	return result.String()
}

func (s *PGSkillStore) GetSkill(name string) (*store.SkillInfo, bool) {
	var id uuid.UUID
	var skillName, slug, visibility string
	var desc *string
	var tags []string
	var version int
	var isSystem bool
	var filePath *string
	err := s.db.QueryRow(
		"SELECT id, name, slug, description, visibility, tags, version, is_system, file_path FROM skills WHERE slug = $1 AND status = 'active'", name,
	).Scan(&id, &skillName, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &filePath)
	if err != nil {
		return nil, false
	}
	info := buildSkillInfo(id.String(), skillName, slug, desc, version, s.baseDir, filePath)
	info.Visibility = visibility
	info.Tags = tags
	info.IsSystem = isSystem
	return &info, true
}

func (s *PGSkillStore) FilterSkills(allowList []string) []store.SkillInfo {
	all := s.ListSkills()
	var filtered []store.SkillInfo
	if allowList == nil {
		// No allowList → return all enabled skills (for agent injection)
		for _, sk := range all {
			if sk.Enabled {
				filtered = append(filtered, sk)
			}
		}
		return filtered
	}
	if len(allowList) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowed[name] = true
	}
	for _, sk := range all {
		if sk.Enabled && allowed[sk.Slug] {
			filtered = append(filtered, sk)
		}
	}
	return filtered
}

// GetSkillByID returns a SkillInfo for any skill by UUID, regardless of status or enabled flag.
// Used by admin operations (e.g. toggle) that need full skill info.
func (s *PGSkillStore) GetSkillByID(id uuid.UUID) (store.SkillInfo, bool) {
	var name, slug, visibility, status string
	var desc *string
	var tags []string
	var version int
	var isSystem, enabled bool
	var depsRaw []byte
	var filePath *string
	err := s.db.QueryRow(
		`SELECT name, slug, description, visibility, tags, version, is_system, status, enabled, deps, file_path
		 FROM skills WHERE id = $1`,
		id,
	).Scan(&name, &slug, &desc, &visibility, pq.Array(&tags), &version, &isSystem, &status, &enabled, &depsRaw, &filePath)
	if err != nil {
		return store.SkillInfo{}, false
	}
	info := buildSkillInfo(id.String(), name, slug, desc, version, s.baseDir, filePath)
	info.Visibility = visibility
	info.Tags = tags
	info.IsSystem = isSystem
	info.Status = status
	info.Enabled = enabled
	info.MissingDeps = parseDepsColumn(depsRaw)
	return info, true
}

// GetSkillOwnerID returns the owner_id for a skill by UUID.
// Returns ("", false) if the skill does not exist.
func (s *PGSkillStore) GetSkillOwnerID(id uuid.UUID) (string, bool) {
	var ownerID string
	err := s.db.QueryRow("SELECT owner_id FROM skills WHERE id = $1", id).Scan(&ownerID)
	if err != nil {
		return "", false
	}
	return ownerID, true
}

// GetSkillOwnerIDBySlug returns the owner_id for a skill by slug.
// Returns ("", false) if the skill does not exist or is archived.
func (s *PGSkillStore) GetSkillOwnerIDBySlug(slug string) (string, bool) {
	var ownerID string
	err := s.db.QueryRow("SELECT owner_id FROM skills WHERE slug = $1 AND status = 'active'", slug).Scan(&ownerID)
	if err != nil {
		return "", false
	}
	return ownerID, true
}

// buildSkillInfo constructs a SkillInfo. If filePath (from DB file_path column) is set,
// it is used as BaseDir; otherwise BaseDir is constructed from baseDir/slug/version.
// This ensures skills seeded under a previous data directory are still resolved correctly.
func buildSkillInfo(id, name, slug string, desc *string, version int, baseDir string, filePath *string) store.SkillInfo {
	d := ""
	if desc != nil {
		d = *desc
	}
	skillDir := fmt.Sprintf("%s/%s/%d", baseDir, slug, version)
	if filePath != nil && *filePath != "" {
		skillDir = *filePath
	}
	return store.SkillInfo{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Path:        skillDir + "/SKILL.md",
		BaseDir:     skillDir,
		Source:      "managed",
		Description: d,
		Version:     version,
	}
}

// readSkillFile reads a SKILL.md file, normalizes line endings, and strips frontmatter.
func readSkillFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = skillFrontmatterRe.ReplaceAllString(content, "")
	return content, nil
}
