package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SkillManifest holds dependency info for a skill.
// Populated by ScanSkillDeps via static analysis of scripts/ directory.
type SkillManifest struct {
	Requires       []string `json:"requires,omitempty"`        // system binaries (python3, pandoc, ffmpeg)
	RequiresPython []string `json:"requires_python,omitempty"` // raw Python import names (e.g. "openpyxl", "cv2")
	RequiresNode   []string `json:"requires_node,omitempty"`   // npm package names (e.g. "docx", "pptxgenjs")
	ScriptsDir     string   `json:"-"`                         // absolute path to scripts/ dir, used for PYTHONPATH
}

// IsEmpty returns true if the manifest has no dependencies.
func (m *SkillManifest) IsEmpty() bool {
	return len(m.Requires) == 0 && len(m.RequiresPython) == 0 && len(m.RequiresNode) == 0
}

// ScanSkillDeps auto-detects dependencies by statically analyzing the scripts/ directory.
func ScanSkillDeps(skillDir string) *SkillManifest {
	return scanScriptsDir(filepath.Join(skillDir, "scripts"))
}

// scanScriptsDir statically analyzes script files to detect dependencies.
// Local module directories (subdirs of scriptsDir) are excluded from pyImports;
// stdlib/pip resolution is handled at check time via PYTHONPATH.
func scanScriptsDir(scriptsDir string) *SkillManifest {
	m := &SkillManifest{ScriptsDir: scriptsDir}

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return m
	}

	pyImports := make(map[string]bool)
	nodeImports := make(map[string]bool)
	binaries := make(map[string]bool)
	// Track subdirectory names — these are local modules and must never be reported as missing.
	localModules := make(map[string]bool)
	// The scripts directory itself can be referenced as a module (e.g. "from scripts import utils").
	localModules[filepath.Base(scriptsDir)] = true

	for _, e := range entries {
		if e.IsDir() {
			localModules[e.Name()] = true
			// Recurse one level into subdirectories
			subEntries, err := os.ReadDir(filepath.Join(scriptsDir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					// Track nested subdirs as local modules too (e.g. office/helpers, office/validators)
					// so intra-package imports like "from helpers import ..." don't get falsely reported.
					localModules[se.Name()] = true
					// Scan files inside nested subdirs
					nestedEntries, err := os.ReadDir(filepath.Join(scriptsDir, e.Name(), se.Name()))
					if err != nil {
						continue
					}
					for _, ne := range nestedEntries {
						if !ne.IsDir() {
							scanFile(filepath.Join(scriptsDir, e.Name(), se.Name(), ne.Name()), pyImports, nodeImports, binaries)
						}
					}
					continue
				}
				scanFile(filepath.Join(scriptsDir, e.Name(), se.Name()), pyImports, nodeImports, binaries)
			}
			continue
		}
		// Track sibling .py files as local modules so cross-file imports
		// (e.g. "from extract_form_field_info import ...") are not reported as pip deps.
		if strings.HasSuffix(e.Name(), ".py") {
			localModules[strings.TrimSuffix(e.Name(), ".py")] = true
		}
		scanFile(filepath.Join(scriptsDir, e.Name()), pyImports, nodeImports, binaries)
	}

	for b := range binaries {
		m.Requires = append(m.Requires, b)
	}
	// Store raw import names — skip local module dirs (subdirs of scriptsDir).
	// dep_checker.go handles stdlib/pip resolution via PYTHONPATH.
	for pkg := range pyImports {
		if !localModules[pkg] {
			m.RequiresPython = append(m.RequiresPython, pkg)
		}
	}
	for pkg := range nodeImports {
		m.RequiresNode = append(m.RequiresNode, pkg)
	}

	// Auto-detect runtime from file extensions
	if len(pyImports) > 0 && !binaries["python3"] {
		m.Requires = append(m.Requires, "python3")
	}
	if len(nodeImports) > 0 && !binaries["node"] {
		m.Requires = append(m.Requires, "node")
	}

	return m
}

var (
	pyImportRe     = regexp.MustCompile(`^import\s+(\w+)`)
	pyFromRe       = regexp.MustCompile(`^from\s+(\w+)`)
	nodeRequireRe  = regexp.MustCompile(`require\(['"]([\w@][^'"]*)['"]\)`)
	nodeESImportRe = regexp.MustCompile(`from\s+['"]([^'"./][^'"]*?)['"]`)
	shebangRe      = regexp.MustCompile(`^#!\s*/usr/bin/env\s+(\S+)`)
)

func scanFile(path string, pyImports, nodeImports map[string]bool, binaries map[string]bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	ext := filepath.Ext(path)

	// Check shebang
	if strings.HasPrefix(content, "#!") {
		firstLine := strings.SplitN(content, "\n", 2)[0]
		if m := shebangRe.FindStringSubmatch(firstLine); len(m) > 1 {
			binaries[m[1]] = true
		}
	}

	switch ext {
	case ".py":
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if m := pyImportRe.FindStringSubmatch(line); len(m) > 1 {
				pyImports[m[1]] = true
			}
			if m := pyFromRe.FindStringSubmatch(line); len(m) > 1 {
				pyImports[m[1]] = true
			}
		}
	case ".js", ".mjs":
		for _, m := range nodeRequireRe.FindAllStringSubmatch(content, -1) {
			if len(m) > 1 {
				nodeImports[normalizeNodePkg(m[1])] = true
			}
		}
		for _, m := range nodeESImportRe.FindAllStringSubmatch(content, -1) {
			if len(m) > 1 {
				nodeImports[normalizeNodePkg(m[1])] = true
			}
		}
	}
}

func normalizeNodePkg(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		parts := strings.SplitN(pkg, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return pkg
	}
	return strings.SplitN(pkg, "/", 2)[0]
}

// MergeDeps merges two manifests, deduplicating entries.
func MergeDeps(a, b *SkillManifest) *SkillManifest {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &SkillManifest{
		Requires:       mergeUnique(a.Requires, b.Requires),
		RequiresPython: mergeUnique(a.RequiresPython, b.RequiresPython),
		RequiresNode:   mergeUnique(a.RequiresNode, b.RequiresNode),
	}
}

func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var result []string
	for _, s := range append(a, b...) {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
