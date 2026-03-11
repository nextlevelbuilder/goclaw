package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SkillManifest holds dependency info for a skill.
// Can come from explicit skill-manifest.json OR auto-scan of scripts/.
type SkillManifest struct {
	Requires       []string `json:"requires,omitempty"`        // system binaries (python3, pandoc, ffmpeg)
	RequiresPython []string `json:"requires_python,omitempty"` // pip packages (openpyxl, pandas)
	RequiresNode   []string `json:"requires_node,omitempty"`   // npm packages (docx, pptxgenjs)
}

// IsEmpty returns true if the manifest has no dependencies.
func (m *SkillManifest) IsEmpty() bool {
	return len(m.Requires) == 0 && len(m.RequiresPython) == 0 && len(m.RequiresNode) == 0
}

// ScanSkillDeps auto-detects dependencies by analyzing a skill directory.
// 1. If skill-manifest.json exists → parse and use it (explicit override)
// 2. Otherwise → static scan of scripts/ directory
func ScanSkillDeps(skillDir string) *SkillManifest {
	// Tier 1: Check for explicit manifest
	manifestPath := filepath.Join(skillDir, "skill-manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var m SkillManifest
		if json.Unmarshal(data, &m) == nil {
			return &m
		}
	}

	// Tier 2: Static analysis of scripts/ directory
	return scanScriptsDir(filepath.Join(skillDir, "scripts"))
}

// scanScriptsDir statically analyzes script files to detect dependencies.
func scanScriptsDir(scriptsDir string) *SkillManifest {
	m := &SkillManifest{}

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return m
	}

	pyImports := make(map[string]bool)
	nodeImports := make(map[string]bool)
	binaries := make(map[string]bool)

	for _, e := range entries {
		if e.IsDir() {
			// Recurse into subdirectories
			subEntries, err := os.ReadDir(filepath.Join(scriptsDir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					continue
				}
				scanFile(filepath.Join(scriptsDir, e.Name(), se.Name()), pyImports, nodeImports, binaries)
			}
			continue
		}
		scanFile(filepath.Join(scriptsDir, e.Name()), pyImports, nodeImports, binaries)
	}

	// Convert maps to slices
	for b := range binaries {
		m.Requires = append(m.Requires, b)
	}
	for pkg := range pyImports {
		if pythonStdlib[pkg] {
			continue
		}
		if pipPkg, ok := pythonModuleToPackage[pkg]; ok {
			m.RequiresPython = append(m.RequiresPython, pipPkg)
		} else {
			m.RequiresPython = append(m.RequiresPython, pkg)
		}
	}
	for pkg := range nodeImports {
		if nodeBuiltin[pkg] {
			continue
		}
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
	nodeRequireRe  = regexp.MustCompile(`require\(['"]([^'"./][^'"]*)['"]\)`)
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
				// Extract package name (handle scoped packages @org/pkg)
				pkg := m[1]
				if strings.HasPrefix(pkg, "@") {
					parts := strings.SplitN(pkg, "/", 3)
					if len(parts) >= 2 {
						pkg = parts[0] + "/" + parts[1]
					}
				} else {
					pkg = strings.SplitN(pkg, "/", 2)[0]
				}
				nodeImports[pkg] = true
			}
		}
		for _, m := range nodeESImportRe.FindAllStringSubmatch(content, -1) {
			if len(m) > 1 {
				pkg := m[1]
				if strings.HasPrefix(pkg, "@") {
					parts := strings.SplitN(pkg, "/", 3)
					if len(parts) >= 2 {
						pkg = parts[0] + "/" + parts[1]
					}
				} else {
					pkg = strings.SplitN(pkg, "/", 2)[0]
				}
				nodeImports[pkg] = true
			}
		}
	}
}

// pythonModuleToPackage maps Python import names to their pip package names
// when they differ.
var pythonModuleToPackage = map[string]string{
	"cv2":      "opencv-python",
	"PIL":      "Pillow",
	"yaml":     "pyyaml",
	"sklearn":  "scikit-learn",
	"bs4":      "beautifulsoup4",
	"dateutil": "python-dateutil",
	"dotenv":   "python-dotenv",
	"pptx":     "python-pptx",
	"docx":     "python-docx",
	"attr":     "attrs",
	"gi":       "PyGObject",
}

// pythonStdlib contains common Python standard library modules to skip.
var pythonStdlib = map[string]bool{
	"os": true, "sys": true, "json": true, "re": true, "math": true,
	"pathlib": true, "shutil": true, "subprocess": true, "io": true,
	"collections": true, "itertools": true, "functools": true,
	"typing": true, "enum": true, "abc": true, "copy": true,
	"datetime": true, "time": true, "calendar": true,
	"hashlib": true, "hmac": true, "secrets": true,
	"csv": true, "configparser": true, "argparse": true,
	"logging": true, "warnings": true, "traceback": true,
	"unittest": true, "doctest": true, "pdb": true,
	"string": true, "textwrap": true, "difflib": true,
	"struct": true, "codecs": true, "unicodedata": true,
	"html": true, "xml": true, "urllib": true, "http": true,
	"email": true, "mimetypes": true,
	"threading": true, "multiprocessing": true, "concurrent": true,
	"socket": true, "ssl": true, "select": true,
	"signal": true, "contextlib": true, "dataclasses": true,
	"tempfile": true, "glob": true, "fnmatch": true,
	"stat": true, "fileinput": true, "linecache": true,
	"pickle": true, "shelve": true, "sqlite3": true,
	"zipfile": true, "tarfile": true, "gzip": true, "bz2": true, "lzma": true,
	"base64": true, "binascii": true, "random": true,
	"statistics": true, "decimal": true, "fractions": true,
	"operator": true, "inspect": true, "dis": true,
	"importlib": true, "pkgutil": true, "pprint": true,
	"platform": true, "ctypes": true, "array": true,
	"queue": true, "heapq": true, "bisect": true,
	"weakref": true, "types": true, "numbers": true,
	"cmath": true, "locale": true, "gettext": true,
	"ast": true, "token": true, "tokenize": true,
}

// nodeBuiltin contains Node.js built-in modules to skip.
var nodeBuiltin = map[string]bool{
	"fs": true, "path": true, "os": true, "http": true, "https": true,
	"url": true, "util": true, "events": true, "stream": true,
	"crypto": true, "buffer": true, "child_process": true,
	"cluster": true, "dgram": true, "dns": true, "net": true,
	"readline": true, "tls": true, "tty": true, "v8": true,
	"vm": true, "zlib": true, "assert": true, "console": true,
	"process": true, "querystring": true, "string_decoder": true,
	"timers": true, "worker_threads": true, "perf_hooks": true,
	"async_hooks": true, "module": true,
	"node:fs": true, "node:path": true, "node:os": true,
	"node:http": true, "node:https": true, "node:url": true,
	"node:util": true, "node:crypto": true, "node:stream": true,
	"node:child_process": true, "node:buffer": true,
	"node:events": true, "node:net": true, "node:zlib": true,
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
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
