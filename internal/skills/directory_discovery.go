// Package skills — Directory walk discovery (CP-07).
// Walks UP from a touched file to find .goclaw/skills/ directories at each level.
// Deeper directories have higher priority (more specific).
package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DiscoveredSkillDir represents a skill directory found by walking up.
type DiscoveredSkillDir struct {
	Path     string // absolute path to the skills directory
	Depth    int    // 0 = closest to file, increases going up
	Priority int    // higher = more specific (= depth for now)
}

// DiscoverSkillsForPath walks UP from a file path to find .goclaw/skills/ directories.
//
// Example: touching /project/packages/auth/handler.go discovers:
//
//	/project/packages/auth/.goclaw/skills/   (depth 0, highest priority)
//	/project/packages/.goclaw/skills/        (depth 1)
//	/project/.goclaw/skills/                 (depth 2, lowest priority)
//
// Security: gitignored directories are skipped (prevents supply-chain injection
// via node_modules/.goclaw/skills/).
func DiscoverSkillsForPath(filePath string, workspaceRoot string) []DiscoveredSkillDir {
	var dirs []DiscoveredSkillDir
	seen := make(map[string]bool)

	// Normalize paths
	workspaceRoot = filepath.Clean(workspaceRoot)
	filePath = filepath.Clean(filePath)
	dir := filepath.Dir(filePath)
	depth := 0

	for {
		// Don't go above workspace root
		if !strings.HasPrefix(dir, workspaceRoot) || dir == "/" || dir == "." {
			break
		}

		skillDir := filepath.Join(dir, ".goclaw", "skills")
		if info, err := os.Stat(skillDir); err == nil && info.IsDir() {
			if !seen[skillDir] {
				seen[skillDir] = true

				if !isGitignored(skillDir, workspaceRoot) {
					dirs = append(dirs, DiscoveredSkillDir{
						Path:     skillDir,
						Depth:    depth,
						Priority: depth,
					})
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
		depth++
	}

	return dirs
}

// DiscoverSkillsForPaths runs discovery for multiple file paths and deduplicates.
func DiscoverSkillsForPaths(filePaths []string, workspaceRoot string) []DiscoveredSkillDir {
	seen := make(map[string]bool)
	var all []DiscoveredSkillDir

	for _, fp := range filePaths {
		dirs := DiscoverSkillsForPath(fp, workspaceRoot)
		for _, d := range dirs {
			if !seen[d.Path] {
				seen[d.Path] = true
				all = append(all, d)
			}
		}
	}

	return all
}

// isGitignored checks if a path is in .gitignore using git check-ignore.
func isGitignored(path string, root string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-q", path)
	err := cmd.Run()
	return err == nil // exit 0 = ignored
}
