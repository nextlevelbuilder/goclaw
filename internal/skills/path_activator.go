// Package skills — Path-based conditional skill activation (CP-07).
// Skills auto-activate when agent touches files matching path patterns.
package skills

import (
	"path/filepath"
	"strings"
	"sync"
)

// PathActivator watches file accesses and activates matching skills.
type PathActivator struct {
	mu    sync.RWMutex
	rules map[string][]string // skill slug → glob patterns from frontmatter
}

// NewPathActivator creates an empty activator.
func NewPathActivator() *PathActivator {
	return &PathActivator{rules: make(map[string][]string)}
}

// Register adds path rules from a skill's frontmatter `paths` field.
func (pa *PathActivator) Register(slug string, patterns []string) {
	if len(patterns) == 0 {
		return
	}
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.rules[slug] = patterns
}

// Unregister removes a skill's path rules.
func (pa *PathActivator) Unregister(slug string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	delete(pa.rules, slug)
}

// ActivateForPaths returns skill slugs that match any of the touched paths.
// Deduplicates results.
func (pa *PathActivator) ActivateForPaths(touchedPaths []string) []string {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	seen := make(map[string]bool)
	var activated []string

	for slug, patterns := range pa.rules {
		if seen[slug] {
			continue
		}
		if matchesAny(patterns, touchedPaths) {
			activated = append(activated, slug)
			seen[slug] = true
		}
	}

	return activated
}

// RuleCount returns the number of registered skills.
func (pa *PathActivator) RuleCount() int {
	pa.mu.RLock()
	defer pa.mu.RUnlock()
	return len(pa.rules)
}

// matchesAny checks if any touched path matches any of the patterns.
func matchesAny(patterns, paths []string) bool {
	for _, pattern := range patterns {
		for _, path := range paths {
			if matchPath(pattern, path) {
				return true
			}
		}
	}
	return false
}

// matchPath handles both standard glob and ** doublestar patterns.
func matchPath(pattern, path string) bool {
	// Normalize separators
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	// Handle ** (doublestar) by checking if basename matches
	if strings.Contains(pattern, "**") {
		// Split pattern at **
		parts := strings.SplitN(pattern, "**", 2)
		prefix := strings.TrimRight(parts[0], "/")
		suffix := ""
		if len(parts) > 1 {
			suffix = strings.TrimLeft(parts[1], "/")
		}

		// Check prefix match (if non-empty)
		if prefix != "" && !strings.Contains(path, prefix) {
			return false
		}

		// Check suffix match (basename or subpath)
		if suffix != "" {
			if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
				return true
			}
			// Also try matching against the full remaining path
			idx := strings.Index(path, prefix)
			if idx >= 0 {
				remaining := path[idx+len(prefix):]
				remaining = strings.TrimLeft(remaining, "/")
				if matched, _ := filepath.Match(suffix, remaining); matched {
					return true
				}
			}
		} else {
			// Pattern ends with ** → matches everything under prefix
			return prefix == "" || strings.Contains(path, prefix)
		}
		return false
	}

	// Standard glob match on basename
	if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
		return true
	}

	// Standard glob match on full path
	if matched, _ := filepath.Match(pattern, path); matched {
		return true
	}

	return false
}
