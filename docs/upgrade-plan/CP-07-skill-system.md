# CP-07: Skill System Upgrade

**Patterns**: #11 (Conditional Activation) + #12 (Shell-in-Prompt) + #13 (Directory Walk)
**Priority**: MEDIUM
**Dependencies**: None
**Estimated effort**: 2 weeks
**Branch**: `feature/cp-07-skill-system`

---

## Objective

Upgrade GoClaw's DB-backed skill system with 3 features from Claude Code:
1. Path-based conditional activation (skills auto-activate when touching matching files)
2. Shell-in-prompt (inline shell commands in SKILL.md executed at load time)
3. Directory walk discovery (walk up from touched file to find `.goclaw/skills/`)

---

## Step 1: Path-based Conditional Activation

### 1.1 Add `paths` to SKILL.md frontmatter

```yaml
---
name: database-migration
description: Guide for writing database migrations
paths: ["**/migrations/**", "**/migrate/**", "*.sql"]
---
```

### 1.2 Create `internal/skills/path_activator.go`

```go
package skills

import (
	"path/filepath"
	"sync"
)

// PathActivator watches file touches and activates matching skills.
type PathActivator struct {
	mu    sync.RWMutex
	rules map[string][]string // skill slug → glob patterns from frontmatter
}

func NewPathActivator() *PathActivator {
	return &PathActivator{rules: make(map[string][]string)}
}

// Register adds path rules from a skill's frontmatter.
func (pa *PathActivator) Register(slug string, patterns []string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.rules[slug] = patterns
}

// ActivateForPaths returns skill slugs that match any of the touched paths.
func (pa *PathActivator) ActivateForPaths(touchedPaths []string) []string {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	seen := map[string]bool{}
	var activated []string

	for slug, patterns := range pa.rules {
		if seen[slug] {
			continue
		}
		for _, pattern := range patterns {
			for _, path := range touchedPaths {
				matched, _ := filepath.Match(pattern, filepath.Base(path))
				if !matched {
					// Try doublestar matching for ** patterns
					matched = matchDoublestar(pattern, path)
				}
				if matched {
					activated = append(activated, slug)
					seen[slug] = true
					break
				}
			}
			if seen[slug] {
				break
			}
		}
	}

	return activated
}

// matchDoublestar handles ** glob patterns.
func matchDoublestar(pattern, path string) bool {
	// Simple implementation: if pattern contains **, split and check
	// For production, use github.com/bmatcuk/doublestar
	// This is a placeholder for the concept
	if !strings.Contains(pattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}
	// ... full doublestar implementation ...
	return false
}
```

### 1.3 Integrate into tool execution callbacks

When a tool touches a file (FileRead, FileWrite, FileEdit, exec), report the path:

```go
// In tool callbacks after file access:
if pa := deps.PathActivator; pa != nil {
	newSkills := pa.ActivateForPaths([]string{touchedFilePath})
	for _, slug := range newSkills {
		// Inject skill into conversation if not already active
		if !state.Context.ActiveSkills[slug] {
			skillContent := deps.SkillLoader.Load(slug)
			state.Messages.AppendPending(Message{
				Role:    "system",
				Content: fmt.Sprintf("<activated-skill name=%q>\n%s\n</activated-skill>", slug, skillContent),
			})
			state.Context.ActiveSkills[slug] = true
		}
	}
}
```

---

## Step 2: Shell-in-Prompt

### 2.1 Create `internal/skills/shell_executor.go`

```go
package skills

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var shellPattern = regexp.MustCompile("!`([^`]+)`")

// ExecuteShellInPrompt replaces !`command` patterns in skill markdown
// with the command's output. Only for local/trusted skills.
//
// SECURITY: MCP-sourced skills MUST NOT have shell commands executed.
// The source parameter controls this.
func ExecuteShellInPrompt(markdown string, source string, workDir string) string {
	// Block remote/untrusted sources
	if source == "mcp" || source == "remote" || source == "plugin" {
		return stripShellCommands(markdown)
	}

	return shellPattern.ReplaceAllStringFunc(markdown, func(match string) string {
		// Extract command between !` and `
		cmd := match[2 : len(match)-1]

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		proc := exec.CommandContext(ctx, "sh", "-c", cmd)
		proc.Dir = workDir
		out, err := proc.Output()
		if err != nil {
			return fmt.Sprintf("[shell error: %v]", err)
		}

		result := strings.TrimSpace(string(out))
		// Limit output size to prevent prompt bloat
		if len(result) > 2000 {
			result = result[:2000] + "\n... (truncated)"
		}
		return result
	})
}

// stripShellCommands removes !`...` patterns from untrusted markdown.
func stripShellCommands(markdown string) string {
	return shellPattern.ReplaceAllString(markdown, "[shell command removed — untrusted source]")
}
```

### 2.2 Integrate into skill loading

**File**: `internal/skills/loader.go`

After loading SKILL.md content, apply shell execution:

```go
func (l *Loader) LoadSkill(slug string) (*SkillContent, error) {
	info := l.cache[slug]
	raw, err := os.ReadFile(info.Path)
	if err != nil { return nil, err }

	content := string(raw)
	frontmatter, body := parseFrontmatter(content)

	// NEW: Execute shell-in-prompt
	body = ExecuteShellInPrompt(body, info.Source, info.BaseDir)

	return &SkillContent{
		Info: info,
		Frontmatter: frontmatter,
		Body: body,
	}, nil
}
```

---

## Step 3: Directory Walk Discovery

### 3.1 Create `internal/skills/directory_discovery.go`

```go
package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// DiscoverSkillsForPath walks UP from a file path to find .goclaw/skills/ directories.
// Deeper directories have higher priority (more specific).
//
// Example: touching /project/packages/auth/handler.go discovers:
//   /project/packages/auth/.goclaw/skills/  (priority 1 — most specific)
//   /project/packages/.goclaw/skills/       (priority 2)
//   /project/.goclaw/skills/                (priority 3 — least specific)
func DiscoverSkillsForPath(filePath string, workspaceRoot string) []DiscoveredSkillDir {
	var dirs []DiscoveredSkillDir
	seen := map[string]bool{}

	dir := filepath.Dir(filePath)
	depth := 0

	for {
		// Don't go above workspace root
		if !strings.HasPrefix(dir, workspaceRoot) || dir == "/" {
			break
		}

		skillDir := filepath.Join(dir, ".goclaw", "skills")
		if info, err := os.Stat(skillDir); err == nil && info.IsDir() {
			if !seen[skillDir] {
				seen[skillDir] = true

				// Check not gitignored
				if !isGitignored(skillDir, workspaceRoot) {
					dirs = append(dirs, DiscoveredSkillDir{
						Path:     skillDir,
						Depth:    depth,
						Priority: depth, // deeper = higher priority
					})
				}
			}
		}

		dir = filepath.Dir(dir)
		depth++
	}

	return dirs
}

type DiscoveredSkillDir struct {
	Path     string
	Depth    int
	Priority int
}

// isGitignored checks if a path is in .gitignore.
// Prevents supply-chain injection via node_modules/.goclaw/skills/
func isGitignored(path string, root string) bool {
	cmd := exec.CommandContext(context.Background(), "git", "-C", root,
		"check-ignore", "-q", path)
	return cmd.Run() == nil // exit 0 = ignored
}
```

### 3.2 Integrate into skill search

When tool touches a file, discover skills from that file's directory tree:

```go
// After file touch in tool callbacks:
if discoveredDirs := DiscoverSkillsForPath(filePath, workspaceRoot); len(discoveredDirs) > 0 {
	for _, dir := range discoveredDirs {
		newSkills := loadSkillsFromDir(dir.Path)
		for _, skill := range newSkills {
			if !loader.Has(skill.Slug) {
				loader.RegisterDynamic(skill)
			}
		}
	}
}
```

---

## Verification Checklist

### Path Activation
- [ ] Skill with `paths: ["**/*.sql"]` activates when agent reads `.sql` file
- [ ] Skill activates only once per session (not re-injected)
- [ ] Multiple skills can activate for same file path

### Shell-in-Prompt
- [ ] `!`git branch --show-current`` replaced with actual branch name
- [ ] MCP-sourced skills: shell commands stripped (security)
- [ ] Command timeout: 5 seconds max
- [ ] Output truncated at 2000 chars

### Directory Walk
- [ ] Walk up from touched file finds `.goclaw/skills/` at each level
- [ ] Deeper directories have higher priority
- [ ] gitignored directories skipped (node_modules protection)
- [ ] Does not walk above workspace root
