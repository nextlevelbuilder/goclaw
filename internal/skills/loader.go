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
	"context"
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
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Paths       []string `json:"paths,omitempty"`
}

// Info describes a discovered skill.
type Info struct {
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`    // directory name (unique identifier)
	Path        string   `json:"path"`    // absolute path to SKILL.md
	BaseDir     string   `json:"baseDir"` // skill directory (parent of SKILL.md)
	Source      string   `json:"source"`  // "workspace", "global", "builtin"
	Description string   `json:"description"`
	Paths       []string `json:"paths,omitempty"`
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

	// DB-managed skills directory (set via SetManagedDir).
	// Uses versioned subdirectory structure: <dir>/<slug>/<version>/SKILL.md
	managedSkillsDir string

	mu            sync.RWMutex
	cache         map[string]*Info // name → info (lazily populated)
	pathActivator *PathActivator

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
		pathActivator:       NewPathActivator(),
	}
}

// SetManagedDir sets the managed skills directory (skills-store).
// Managed skills use versioned subdirectories: <dir>/<slug>/<version>/SKILL.md.
// Called after PG stores are created.
func (l *Loader) SetManagedDir(dir string) {
	l.managedSkillsDir = dir
	l.BumpVersion() // trigger re-scan
}

// ListSkills returns all available skills, respecting the priority hierarchy.
// Higher-priority sources override lower ones by name.
func (l *Loader) ListSkills(_ context.Context) []Info {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cache = make(map[string]*Info)
	l.pathActivator = NewPathActivator()

	seen := make(map[string]bool)
	var skills []Info

	// Priority: workspace > project-agents > personal-agents > global > managed > builtin
	// Managed (DB-seeded) skills take priority over raw bundled files so agents
	// always receive paths within the skills-store (workspace-accessible), not /app/bundled-skills/.
	for _, src := range []struct {
		dir    string
		source string
	}{
		{l.workspaceSkills, "workspace"},
		{l.projectAgentSkills, "agents-project"},
		{l.personalAgentSkills, "agents-personal"},
		{l.globalSkills, "global"},
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
				info.Paths = append([]string(nil), meta.Paths...)
			}
			skills = append(skills, info)
			seen[d.Name()] = true
			l.cache[d.Name()] = &info
			l.pathActivator.Register(info.Slug, info.Paths)
		}
	}

	// Managed skills (versioned, DB-seeded) come before builtin so their workspace paths win.
	if l.managedSkillsDir != "" {
		for _, info := range l.listManagedSkills() {
			if seen[info.Slug] {
				continue
			}
			skills = append(skills, info)
			seen[info.Slug] = true
			l.cache[info.Slug] = &info
			l.pathActivator.Register(info.Slug, info.Paths)
		}
	}

	// Builtin (raw bundled files) — lowest priority fallback.
	if l.builtinSkills != "" {
		dirs, err := os.ReadDir(l.builtinSkills)
		if err == nil {
			for _, d := range dirs {
				if !d.IsDir() || seen[d.Name()] {
					continue
				}
				skillFile := filepath.Join(l.builtinSkills, d.Name(), "SKILL.md")
				if _, err := os.Stat(skillFile); err != nil {
					continue
				}
				info := Info{
					Name:    d.Name(),
					Slug:    d.Name(),
					Path:    skillFile,
					BaseDir: filepath.Join(l.builtinSkills, d.Name()),
					Source:  "builtin",
				}
				if meta := parseMetadata(skillFile); meta != nil {
					info.Description = meta.Description
					if meta.Name != "" {
						info.Name = meta.Name
					}
					info.Paths = append([]string(nil), meta.Paths...)
				}
				skills = append(skills, info)
				seen[d.Name()] = true
				l.cache[d.Name()] = &info
				l.pathActivator.Register(info.Slug, info.Paths)
			}
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
			info.Paths = append([]string(nil), meta.Paths...)
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
// Priority: workspace > agents > global > managed > builtin
func (l *Loader) LoadSkill(_ context.Context, name string) (string, bool) {
	l.ListSkills(context.Background())
	l.mu.RLock()
	info, ok := l.cache[name]
	l.mu.RUnlock()
	if ok {
		return l.renderSkill(*info)
	}
	return "", false
}

// LoadForContext loads multiple skills and formats them for system prompt injection.
// If allowList is nil, all skills are loaded. If non-nil, only listed skills are loaded.
func (l *Loader) LoadForContext(ctx context.Context, allowList []string) string {
	var names []string

	if allowList == nil {
		// Load all available skills
		for _, s := range l.ListSkills(ctx) {
			names = append(names, s.Slug)
		}
	} else {
		names = allowList
	}

	if len(names) == 0 {
		return ""
	}

	var parts []string
	for _, name := range names {
		content, ok := l.LoadSkill(ctx, name)
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

// skillDescMaxLen is the max character length for skill descriptions
// in the system prompt inline XML. ~200 chars ≈ ~50 tokens, balancing
// discoverability with prompt budget. Full SKILL.md is read on actual use.
const skillDescMaxLen = 200

// BuildSummary returns an XML summary of skills for context injection.
// If allowList is nil, all skills are included. If non-nil, only listed skills are included.
// The format matches the TS <available_skills> XML used in system prompts.
func (l *Loader) BuildSummary(ctx context.Context, allowList []string) string {
	allSkills := l.ListSkills(ctx)
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
		desc := s.Description
		if len([]rune(desc)) > skillDescMaxLen {
			desc = string([]rune(desc)[:skillDescMaxLen]) + "…"
		}
		lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapeXML(desc)))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", escapeXML(s.Path)))
		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</available_skills>")

	return strings.Join(lines, "\n")
}

// BuildPinnedSummary generates XML summary for only the pinned skill names.
// Delegates to BuildSummary with pinned names as allowlist.
// Returns empty string if none match.
func (l *Loader) BuildPinnedSummary(ctx context.Context, pinnedNames []string) string {
	if len(pinnedNames) == 0 {
		return ""
	}
	return l.BuildSummary(ctx, pinnedNames)
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
func (l *Loader) FilterSkills(ctx context.Context, allowList []string) []Info {
	all := l.ListSkills(ctx)
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
func (l *Loader) GetSkill(ctx context.Context, name string) (*Info, bool) {
	// Ensure cache is populated
	l.ListSkills(ctx)

	l.mu.RLock()
	defer l.mu.RUnlock()
	info, ok := l.cache[name]
	return info, ok
}

// ActivatedSkillContents returns rendered skill bodies that should be injected
// when the agent touches the given paths. It combines registered path rules
// with directory walk discovery from local .goclaw/skills folders.
func (l *Loader) ActivatedSkillContents(ctx context.Context, touchedPaths []string) map[string]string {
	if len(touchedPaths) == 0 {
		return nil
	}

	l.ListSkills(ctx)

	l.mu.RLock()
	pa := l.pathActivator
	cache := make(map[string]*Info, len(l.cache))
	for k, v := range l.cache {
		cache[k] = v
	}
	l.mu.RUnlock()

	contents := make(map[string]string)
	if pa != nil {
		for _, slug := range pa.ActivateForPaths(touchedPaths) {
			info := cache[slug]
			if info == nil {
				continue
			}
			if content, ok := l.renderSkill(*info); ok {
				contents[slug] = content
			}
		}
	}

	for slug, content := range l.discoverSkillContentsForPaths(touchedPaths) {
		if _, exists := contents[slug]; !exists {
			contents[slug] = content
		}
	}

	if len(contents) == 0 {
		return nil
	}
	return contents
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
		jm.Paths = parsePaths(fm)
		return &jm
	}

	// Fall back to simple YAML key: value
	kv := parseSimpleYAML(fm)
	return &Metadata{
		Name:        kv["name"],
		Description: kv["description"],
		Paths:       parsePaths(fm),
	}
}

// normalizeLineEndings converts \r\n and bare \r to \n so frontmatter regex matches
// files created on Windows or uploaded via ZIP with CRLF line endings.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func extractFrontmatter(content string) string {
	match := frontmatterRe.FindStringSubmatch(normalizeLineEndings(content))
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func stripFrontmatter(content string) string {
	return frontmatterRe.ReplaceAllString(normalizeLineEndings(content), "")
}

// parseSimpleYAML parses a subset of YAML: simple key: value pairs,
// multiline block scalars (| and >), and list values (- item).
func parseSimpleYAML(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")

	var currentKey string
	var blockLines []string
	var inBlock bool

	flushBlock := func() {
		if currentKey != "" {
			if len(blockLines) > 0 {
				result[currentKey] = strings.Join(blockLines, " ")
			} else {
				// Empty value (e.g. "slug:" with no indented continuation).
				result[currentKey] = ""
			}
		}
		currentKey = ""
		blockLines = nil
		inBlock = false
	}

	for _, line := range lines {
		// Indented continuation line (block scalar or list item)
		if inBlock && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			// List item: "  - value"
			if strings.HasPrefix(trimmed, "- ") {
				blockLines = append(blockLines, strings.TrimSpace(trimmed[2:]))
			} else if trimmed != "-" {
				blockLines = append(blockLines, trimmed)
			}
			continue
		}

		// Not indented — flush any pending block and parse as top-level key
		flushBlock()

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")

		if val == "|" || val == ">" || val == "|-" || val == ">-" {
			// Start of a multiline block — collect subsequent indented lines
			currentKey = key
			inBlock = true
			continue
		}
		if val == "" {
			// Could be start of a list block (e.g. "allowed-tools:\n  - Bash")
			currentKey = key
			inBlock = true
			continue
		}
		result[key] = val
	}
	flushBlock()
	return result
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func (l *Loader) renderSkill(info Info) (string, bool) {
	data, err := os.ReadFile(info.Path)
	if err != nil {
		return "", false
	}
	content := stripFrontmatter(string(data))
	content = ExecuteShellInPrompt(content, info.Source, info.BaseDir)
	content = strings.ReplaceAll(content, "{baseDir}", info.BaseDir)
	return content, true
}

func (l *Loader) discoverSkillContentsForPaths(touchedPaths []string) map[string]string {
	workspaceRoot := l.workspaceRoot()
	if workspaceRoot == "" {
		return nil
	}

	dirs := DiscoverSkillsForPaths(touchedPaths, workspaceRoot)
	if len(dirs) == 0 {
		return nil
	}

	contents := make(map[string]string)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir.Path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(dir.Path, entry.Name())
			skillFile := filepath.Join(skillDir, "SKILL.md")
			if _, err := os.Stat(skillFile); err != nil {
				continue
			}

			info := Info{
				Name:    entry.Name(),
				Slug:    entry.Name(),
				Path:    skillFile,
				BaseDir: skillDir,
				Source:  "discovered",
			}
			if meta := parseMetadata(skillFile); meta != nil {
				if meta.Name != "" {
					info.Name = meta.Name
				}
				info.Description = meta.Description
				info.Paths = append([]string(nil), meta.Paths...)
			}
			if len(info.Paths) > 0 && !matchesAny(info.Paths, touchedPaths) {
				continue
			}
			if content, ok := l.renderSkill(info); ok {
				contents[info.Slug] = content
			}
		}
	}
	if len(contents) == 0 {
		return nil
	}
	return contents
}

func (l *Loader) workspaceRoot() string {
	if l.workspaceSkills != "" {
		return filepath.Dir(l.workspaceSkills)
	}
	if l.projectAgentSkills != "" {
		return filepath.Dir(filepath.Dir(l.projectAgentSkills))
	}
	return ""
}

func parsePaths(frontmatter string) []string {
	frontmatter = normalizeLineEndings(frontmatter)
	lines := strings.Split(frontmatter, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "paths:") {
			continue
		}

		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "paths:"))
		if rest != "" {
			return splitPathList(rest)
		}

		var paths []string
		for j := i + 1; j < len(lines); j++ {
			next := lines[j]
			if next == "" {
				continue
			}
			if next[0] != ' ' && next[0] != '\t' {
				break
			}
			entry := strings.TrimSpace(next)
			if strings.HasPrefix(entry, "- ") {
				entry = strings.TrimSpace(strings.TrimPrefix(entry, "- "))
			}
			entry = strings.Trim(entry, "\"'")
			if entry != "" {
				paths = append(paths, entry)
			}
		}
		return paths
	}
	return nil
}

func splitPathList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")

	parts := strings.Split(value, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, "\"'"))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
