// Package skills loads and manages SKILL.md files from multiple source directories.
// Skills are injected into the agent's system prompt to provide specialized knowledge.
//
// Hierarchy (highest priority wins, matching TS loadSkillEntries):
//  1. Workspace skills          — <workspace>/skills/
//  2. Project agent skills      — <workspace>/.agents/skills/
//  3. Personal agent skills     — ~/.agents/skills/
//  4. Global/managed skills     — ~/.goclaw/skills/
//  5. Builtin skills            — bundled with binary
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metadata holds parsed SKILL.md frontmatter.
type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Info describes a discovered skill.
type Info struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`    // directory name (unique identifier)
	Path        string `json:"path"`    // absolute path to SKILL.md
	BaseDir     string `json:"baseDir"` // skill directory (parent of SKILL.md)
	Source      string `json:"source"`  // "workspace", "global", "builtin"
	Description string `json:"description"`
}

// Loader discovers and loads SKILL.md files from multiple directories.
type Loader struct {
	// Skill directories in priority order (highest first).
	// Matches TS loadSkillEntries() 5-tier hierarchy.
	workspaceSkills     string // <workspace>/skills/
	projectAgentSkills  string // <workspace>/.agents/skills/
	personalAgentSkills string // ~/.agents/skills/
	globalSkills        string // ~/.goclaw/skills/
	builtinSkills       string // bundled with binary

	// Managed skills directory (set via SetManagedDir in managed mode).
	// Uses versioned subdirectory structure: <dir>/<slug>/<version>/SKILL.md
	managedSkillsDir string

	mu    sync.RWMutex
	cache map[string]*Info // name → info (lazily populated)

	// Version tracking for hot-reload (matching TS bumpSkillsSnapshotVersion).
	// Bumped by the watcher on SKILL.md changes; consumers compare to detect staleness.
	version atomic.Int64
}

// NewLoader creates a skills loader.
// workspace: project workspace root (skills dir is workspace/skills/)
// globalSkills: global skills directory (e.g. ~/.goclaw/skills)
// builtinSkills: bundled skills directory
func NewLoader(workspace, globalSkills, builtinSkills string) *Loader {
	wsSkills := ""
	projectAgentSkills := ""
	if workspace != "" {
		wsSkills = filepath.Join(workspace, "skills")
		projectAgentSkills = filepath.Join(workspace, ".agents", "skills")
	}

	// Personal agent skills: ~/.agents/skills/ (matching TS)
	homeDir, _ := os.UserHomeDir()
	personalAgentSkills := ""
	if homeDir != "" {
		personalAgentSkills = filepath.Join(homeDir, ".agents", "skills")
	}

	return &Loader{
		workspaceSkills:     wsSkills,
		projectAgentSkills:  projectAgentSkills,
		personalAgentSkills: personalAgentSkills,
		globalSkills:        globalSkills,
		builtinSkills:       builtinSkills,
		cache:               make(map[string]*Info),
	}
}

// SetManagedDir sets the managed skills directory (skills-store).
// Managed skills use versioned subdirectories: <dir>/<slug>/<version>/SKILL.md.
// Called in managed mode after PG stores are created.
func (l *Loader) SetManagedDir(dir string) {
	l.managedSkillsDir = dir
	l.BumpVersion() // trigger re-scan
}

// ListSkills returns all available skills, respecting the priority hierarchy.
// Higher-priority sources override lower ones by name.
func (l *Loader) ListSkills() []Info {
	l.mu.Lock()
	defer l.mu.Unlock()

	seen := make(map[string]bool)
	var skills []Info

	// Priority: workspace > project-agents > personal-agents > global > builtin
	for _, src := range []struct {
		dir    string
		source string
	}{
		{l.workspaceSkills, "workspace"},
		{l.projectAgentSkills, "agents-project"},
		{l.personalAgentSkills, "agents-personal"},
		{l.globalSkills, "global"},
		{l.builtinSkills, "builtin"},
	} {
		if src.dir == "" {
			continue
		}
		dirs, err := os.ReadDir(src.dir)
		if err != nil {
			continue
		}
		for _, d := range dirs {
			if !d.IsDir() || seen[d.Name()] {
				continue
			}
			skillFile := filepath.Join(src.dir, d.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err != nil {
				continue
			}

			info := Info{
				Name:    d.Name(),
				Slug:    d.Name(),
				Path:    skillFile,
				BaseDir: filepath.Join(src.dir, d.Name()),
				Source:  src.source,
			}
			if meta := parseMetadata(skillFile); meta != nil {
				info.Description = meta.Description
				if meta.Name != "" {
					info.Name = meta.Name
				}
			}
			skills = append(skills, info)
			seen[d.Name()] = true
			l.cache[d.Name()] = &info
		}
	}

	// Managed skills: versioned subdirectories <managedSkillsDir>/<slug>/<version>/SKILL.md
	// Only include skills not already seen from higher-priority sources.
	if l.managedSkillsDir != "" {
		for _, info := range l.listManagedSkills() {
			if seen[info.Slug] {
				continue
			}
			skills = append(skills, info)
			seen[info.Slug] = true
			l.cache[info.Slug] = &info
		}
	}

	return skills
}

// listManagedSkills scans the managed skills directory for versioned skill directories.
// Structure: <managedSkillsDir>/<slug>/<version>/SKILL.md
// Returns the latest version of each skill found.
func (l *Loader) listManagedSkills() []Info {
	dirs, err := os.ReadDir(l.managedSkillsDir)
	if err != nil {
		return nil
	}

	var skills []Info
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		slug := d.Name()

		// Find the latest version subdirectory
		latestVersion, latestDir := l.findLatestVersion(slug)
		if latestVersion < 0 {
			continue
		}

		skillFile := filepath.Join(latestDir, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		info := Info{
			Name:    slug,
			Slug:    slug,
			Path:    skillFile,
			BaseDir: latestDir,
			Source:  "managed",
		}
		if meta := parseMetadata(skillFile); meta != nil {
			info.Description = meta.Description
			if meta.Name != "" {
				info.Name = meta.Name
			}
		}
		skills = append(skills, info)
	}
	return skills
}

// findLatestVersion finds the highest-numbered version subdirectory for a skill slug.
// Returns (version, path) or (-1, "") if no valid version found.
func (l *Loader) findLatestVersion(slug string) (int, string) {
	slugDir := filepath.Join(l.managedSkillsDir, slug)
	entries, err := os.ReadDir(slugDir)
	if err != nil {
		return -1, ""
	}

	var versions []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := strconv.Atoi(e.Name())
		if err != nil || v < 1 {
			continue
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		return -1, ""
	}

	sort.Sort(sort.Reverse(sort.IntSlice(versions)))
	latestVer := versions[0]
	return latestVer, filepath.Join(slugDir, strconv.Itoa(latestVer))
}

// LoadSkill reads and returns the content of a skill by name (frontmatter stripped).
// The {baseDir} placeholder in SKILL.md is replaced with the skill's absolute directory path.
func (l *Loader) LoadSkill(name string) (string, bool) {
	// Check standard (flat) skill directories first
	for _, dir := range []string{l.workspaceSkills, l.projectAgentSkills, l.personalAgentSkills, l.globalSkills, l.builtinSkills} {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := stripFrontmatter(string(data))
		baseDir := filepath.Join(dir, name)
		content = strings.ReplaceAll(content, "{baseDir}", baseDir)
		return content, true
	}

	// Check managed skills directory (versioned structure)
	if l.managedSkillsDir != "" {
		latestVer, latestDir := l.findLatestVersion(name)
		if latestVer >= 0 {
			path := filepath.Join(latestDir, "SKILL.md")
			data, err := os.ReadFile(path)
			if err == nil {
				content := stripFrontmatter(string(data))
				content = strings.ReplaceAll(content, "{baseDir}", latestDir)
				return content, true
			}
		}
	}

	return "", false
}

// LoadForContext loads multiple skills and formats them for system prompt injection.
// If allowList is nil, all skills are loaded. If non-nil, only listed skills are loaded.
func (l *Loader) LoadForContext(allowList []string) string {
	var names []string

	if allowList == nil {
		// Load all available skills
		for _, s := range l.ListSkills() {
			names = append(names, s.Name)
		}
	} else {
		names = allowList
	}

	if len(names) == 0 {
		return ""
	}

	var parts []string
	for _, name := range names {
		content, ok := l.LoadSkill(name)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", name, content))
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Available Skills\n\n" + strings.Join(parts, "\n\n---\n\n")
}

// BuildSummary returns an XML summary of skills for context injection.
// If allowList is nil, all skills are included. If non-nil, only listed skills are included.
// The format matches the TS <available_skills> XML used in system prompts.
func (l *Loader) BuildSummary(allowList []string) string {
	allSkills := l.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	// Filter by allowList if provided
	var filtered []Info
	if allowList == nil {
		filtered = allSkills
	} else {
		allowed := make(map[string]bool, len(allowList))
		for _, name := range allowList {
			allowed[name] = true
		}
		for _, s := range allSkills {
			if allowed[s.Slug] {
				filtered = append(filtered, s)
			}
		}
	}

	if len(filtered) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "<available_skills>")
	for _, s := range filtered {
		lines = append(lines, "  <skill>")
		lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapeXML(s.Name)))
		lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapeXML(s.Description)))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", escapeXML(s.Path)))
		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</available_skills>")

	return strings.Join(lines, "\n")
}

// Version returns the current skill snapshot version.
// Consumers compare this to their cached version to detect changes.
func (l *Loader) Version() int64 {
	return l.version.Load()
}

// BumpVersion increments the version counter (called by watcher on changes).
func (l *Loader) BumpVersion() {
	l.version.Store(time.Now().UnixMilli())
}

// Dirs returns all non-empty skill directories (for the watcher to monitor).
func (l *Loader) Dirs() []string {
	var dirs []string
	for _, d := range []string{l.workspaceSkills, l.projectAgentSkills, l.personalAgentSkills, l.globalSkills, l.builtinSkills, l.managedSkillsDir} {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// FilterSkills returns skills filtered by an allowlist.
// If allowList is nil, all skills are returned. If empty slice, none are returned.
func (l *Loader) FilterSkills(allowList []string) []Info {
	all := l.ListSkills()
	if allowList == nil {
		return all
	}
	if len(allowList) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowed[name] = true
	}
	var filtered []Info
	for _, s := range all {
		if allowed[s.Slug] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// GetSkill returns info about a specific skill.
func (l *Loader) GetSkill(name string) (*Info, bool) {
	// Ensure cache is populated
	l.ListSkills()

	l.mu.RLock()
	defer l.mu.RUnlock()
	info, ok := l.cache[name]
	return info, ok
}

// --- Frontmatter parsing ---

var frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)

func parseMetadata(path string) *Metadata {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	fm := extractFrontmatter(string(data))
	if fm == "" {
		return &Metadata{Name: filepath.Base(filepath.Dir(path))}
	}

	// Try JSON first
	var jm Metadata
	if json.Unmarshal([]byte(fm), &jm) == nil && jm.Name != "" {
		return &jm
	}

	// Fall back to simple YAML key: value
	kv := parseSimpleYAML(fm)
	return &Metadata{
		Name:        kv["name"],
		Description: kv["description"],
	}
}

func extractFrontmatter(content string) string {
	match := frontmatterRe.FindStringSubmatch(content)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func stripFrontmatter(content string) string {
	return frontmatterRe.ReplaceAllString(content, "")
}

func parseSimpleYAML(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, "\"'")
			result[key] = val
		}
	}
	return result
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
