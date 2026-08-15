package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Identifier allowlists for manifest-declared deps. Manifest strings flow
// into python3/node subprocesses via fmt.Sprintf — an unvalidated name would
// let a SKILL.md author inject arbitrary code (e.g. "foo;__import__('os').system(...)").
// Auto-scan already sanitizes via regex capture (\w+); these guards only apply
// to manifest-origin data.
//
// Note: python import allows hyphen/dot even though "import psycopg2-binary"
// yields a SyntaxError at python parse time — that's a SAFE failure (no exec),
// and the installer still treats the package as missing → installs it, matching
// the author's intent when declaring pip install names like "psycopg2-binary".
var (
	pythonIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*$`)
	npmPkgNameRe  = regexp.MustCompile(`^(@[a-z0-9][a-z0-9_.\-]*/)?[a-z0-9][a-z0-9_.\-]*$`)
	sysBinRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]*$`)
)

// isValidDepName returns true when name passes the per-category allowlist.
// Called before manifest strings reach exec subprocesses. Empty names fail.
func isValidDepName(category, name string) bool {
	if name == "" {
		return false
	}
	switch category {
	case "pip":
		return pythonIdentRe.MatchString(name)
	case "npm":
		return npmPkgNameRe.MatchString(name)
	case "system":
		return sysBinRe.MatchString(name)
	case "github":
		return true // github spec validated by ParseGitHubSpec at install time
	}
	return false
}

// ParsedDep describes a manifest-declared dependency after prefix categorization.
type ParsedDep struct {
	Raw         string // original manifest string (e.g. "pip:requests>=2.31")
	Category    string // "pip" | "npm" | "system" | "github"
	ImportName  string // bare name used for import-check (e.g. "requests")
	InstallSpec string // pass-through string for installer (e.g. "requests>=2.31")
}

// categorizeManifestDep parses a raw manifest dep string into its category
// (pip/npm/system/github) and normalized import/install names.
//
// Accepted forms:
//
//	pip:<spec>       e.g. pip:psycopg2-binary, pip:requests>=2.31, pip:psycopg[binary]
//	npm:<pkg>        e.g. npm:typescript
//	github:<spec>    e.g. github:cli/cli@v2.40.0
//	system:<bin>     e.g. system:ffmpeg
//	<bare-name>      treated as system binary (matches installer default branch)
func categorizeManifestDep(raw string) ParsedDep {
	p := ParsedDep{Raw: raw}
	switch {
	case strings.HasPrefix(raw, "pip:"):
		p.Category = "pip"
		spec := strings.TrimPrefix(raw, "pip:")
		p.ImportName, p.InstallSpec = splitPipSpec(spec)
	case strings.HasPrefix(raw, "npm:"):
		p.Category = "npm"
		p.ImportName = strings.TrimPrefix(raw, "npm:")
		p.InstallSpec = p.ImportName
	case strings.HasPrefix(raw, "github:"):
		p.Category = "github"
		p.InstallSpec = raw
	case strings.HasPrefix(raw, "system:"):
		p.Category = "system"
		p.ImportName = strings.TrimPrefix(raw, "system:")
		p.InstallSpec = p.ImportName
	default:
		p.Category = "system"
		p.ImportName = raw
		p.InstallSpec = raw
	}
	return p
}

// splitPipSpec separates the import-check name from the install spec.
// Strips version operators (>=, <=, ==, !=, ~=, >, <) and pip extras ([binary]).
//
// Returns an empty importName when the spec is malformed (e.g. ">=1.0" with no
// package name, or a leading operator at index 0). Callers MUST check and skip
// empty-name entries to avoid feeding syntax errors into python subprocesses.
//
//	"requests>=2.31"   → ("requests",  "requests>=2.31")
//	"psycopg[binary]"  → ("psycopg",   "psycopg[binary]")
//	"psycopg2-binary"  → ("psycopg2-binary", "psycopg2-binary")
//	">=1.0"            → ("", ">=1.0")           — malformed, skip
//	"[binary]"         → ("", "[binary]")        — malformed, skip
func splitPipSpec(spec string) (importName, installSpec string) {
	installSpec = spec
	importName = spec
	for _, op := range []string{">=", "<=", "==", "!=", "~=", ">", "<"} {
		i := strings.Index(importName, op)
		if i == 0 {
			return "", installSpec
		}
		if i > 0 {
			importName = strings.TrimSpace(importName[:i])
			break
		}
	}
	if i := strings.IndexByte(importName, '['); i == 0 {
		return "", installSpec
	} else if i > 0 {
		importName = importName[:i]
	}
	return importName, installSpec
}

// skillMdPath returns the absolute path to SKILL.md in a skill directory.
func skillMdPath(skillDir string) string {
	return filepath.Join(skillDir, "SKILL.md")
}

// parseSkillManifestFile reads SKILL.md and extracts deps: / exclude_deps:
// lists from its YAML frontmatter. Returns zero slices if file absent,
// frontmatter missing, or fields absent.
func parseSkillManifestFile(skillMdPath string) (deps []string, excludeDeps []string) {
	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		return nil, nil
	}
	fm := extractFrontmatter(string(data))
	if fm == "" {
		return nil, nil
	}
	lists := parseSimpleYAMLLists(fm)
	return lists["deps"], lists["exclude_deps"]
}

// applyManifestOverride merges a scan result with manifest-declared deps.
//
// When explicit deps are present, they become authoritative: scan-derived
// slices (Requires, RequiresPython, RequiresNode) are replaced with
// manifest-categorized entries and FromManifest flips to true.
//
// When only excludeDeps are present, the scan result is filtered in place.
//
// When both are empty, the scan result is returned unchanged.
func applyManifestOverride(scan *SkillManifest, explicit, excludeDeps []string) *SkillManifest {
	if scan == nil {
		scan = &SkillManifest{}
	}
	scan.ExcludeDeps = excludeDeps

	if len(explicit) == 0 {
		if len(excludeDeps) > 0 {
			scan.RequiresPython = filterOutByImportName(scan.RequiresPython, excludeDeps, "pip")
			scan.RequiresNode = filterOutByImportName(scan.RequiresNode, excludeDeps, "npm")
			scan.Requires = filterOutByImportName(scan.Requires, excludeDeps, "system")
		}
		return scan
	}

	scan.FromManifest = true
	scan.Explicit = explicit
	var sysReq, pyReq, nodeReq []string
	for _, raw := range explicit {
		p := categorizeManifestDep(raw)
		if p.Category != "github" && !isValidDepName(p.Category, p.ImportName) {
			slog.Warn("skills: dropping invalid manifest dep",
				"raw", raw, "category", p.Category, "import_name", p.ImportName)
			continue
		}
		switch p.Category {
		case "pip":
			pyReq = append(pyReq, p.ImportName)
		case "npm":
			nodeReq = append(nodeReq, p.ImportName)
		case "system":
			sysReq = append(sysReq, p.ImportName)
		}
	}
	scan.Requires = sysReq
	scan.RequiresPython = pyReq
	scan.RequiresNode = nodeReq
	return scan
}

// filterOutByImportName removes entries whose prefixed form appears in
// excludeDeps. For category "pip"/"npm" the prefix is "<category>:".
// For "system" both "system:<name>" and bare "<name>" are accepted.
func filterOutByImportName(names, excludeDeps []string, category string) []string {
	if len(names) == 0 || len(excludeDeps) == 0 {
		return names
	}
	blocked := make(map[string]bool)
	prefix := category + ":"
	for _, e := range excludeDeps {
		switch {
		case strings.HasPrefix(e, prefix):
			name, _ := splitPipSpec(strings.TrimPrefix(e, prefix))
			if name != "" {
				blocked[name] = true
			}
		case category == "system" && !strings.Contains(e, ":"):
			blocked[e] = true
		}
	}
	if len(blocked) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !blocked[n] {
			out = append(out, n)
		}
	}
	return out
}
