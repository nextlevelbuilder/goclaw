package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	shellwords "github.com/mattn/go-shellwords"
)

// dynamicPathExemptions builds runtime exemptions for the active user's workspace
// upload directories and team workspace root. Only exempts paths that are nested
// under a denied root — paths outside deny roots don't need exemptions.
func (t *ExecTool) dynamicPathExemptions(ctx context.Context) []string {
	var exemptions []string
	seen := make(map[string]struct{}, 4)
	workspace := ToolWorkspaceFromCtx(ctx)
	teamWorkspace := ToolTeamWorkspaceFromCtx(ctx)

	var dirs []string
	if teamWorkspace != "" {
		dirs = append(dirs, teamWorkspace)
	}
	if workspace != "" && filepath.Clean(workspace) != filepath.Clean(teamWorkspace) {
		dirs = append(dirs, filepath.Join(workspace, ".uploads"))
		dirs = append(dirs, filepath.Join(workspace, "uploads"))
	}

	for _, dir := range dirs {
		if dir == "" || strings.Contains(dir, "..") {
			continue
		}
		for _, variant := range pathAliasVariants(filepath.Clean(dir)) {
			if !t.isNestedUnderDeniedRoot(variant) {
				continue
			}
			for _, ex := range []string{variant, variant + string(filepath.Separator)} {
				if _, ok := seen[ex]; ok {
					continue
				}
				seen[ex] = struct{}{}
				exemptions = append(exemptions, ex)
			}
		}
	}
	return exemptions
}

// pathAliasVariants returns the path plus any known runtime alias mappings.
// On the claw server, /app/workspace is symlinked to /app/.goclaw at runtime,
// so both forms may appear in LLM-generated commands for the same physical path.
func pathAliasVariants(path string) []string {
	variants := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	appendVariant := func(v string) {
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		variants = append(variants, v)
	}
	appendVariant(path)
	pathSlash := filepath.ToSlash(path)
	for _, mapping := range [][2]string{
		{"/app/workspace", "/app/.goclaw"},
		{"/app/.goclaw", "/app/workspace"},
	} {
		from, to := mapping[0], mapping[1]
		var mapped string
		if pathSlash == from {
			mapped = to
		} else if strings.HasPrefix(pathSlash, from+"/") {
			mapped = to + strings.TrimPrefix(pathSlash, from)
		}
		if mapped != "" {
			appendVariant(mapped)
			appendVariant(filepath.FromSlash(mapped))
			continue
		}
	}
	return variants
}

// isNestedUnderDeniedRoot checks whether path sits inside any of the configured
// deny roots. Supports both absolute roots (prefix match) and relative roots
// (e.g. ".goclaw/" — checked as a path component marker anywhere in path).
func (t *ExecTool) isNestedUnderDeniedRoot(path string) bool {
	pathClean := filepath.ToSlash(filepath.Clean(path))
	pathWithBoundary := "/" + strings.Trim(pathClean, "/") + "/"
	for _, root := range t.pathDenyRoots {
		cleanRoot := filepath.ToSlash(filepath.Clean(root))
		if cleanRoot == "." || cleanRoot == "/" {
			continue
		}
		if !filepath.IsAbs(root) && !strings.HasPrefix(cleanRoot, "/") {
			marker := "/" + strings.Trim(cleanRoot, "/") + "/"
			if strings.Contains(pathWithBoundary, marker) {
				return true
			}
			continue
		}
		if equalPathString(pathClean, cleanRoot) {
			continue
		}
		if hasPathPrefix(pathClean, cleanRoot) {
			return true
		}
	}
	return false
}

// matchesPathExemption checks if a resolved path falls under any exemption prefix.
func matchesPathExemption(path string, exemptions []string) bool {
	path = normalizePathForMatch(path)
	for _, ex := range exemptions {
		if ex == "" {
			continue
		}
		ex = normalizePathForMatch(ex)
		if equalPathString(path, ex) {
			return true
		}
		if hasPathPrefix(path, ex) {
			return true
		}
	}
	return false
}

func normalizePathForMatch(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean != "/" {
		clean = strings.TrimRight(clean, "/")
	}
	return clean
}

func equalPathString(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func hasPathPrefix(path, prefix string) bool {
	if equalPathString(path, prefix) {
		return true
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		prefix = strings.ToLower(prefix)
	}
	return strings.HasPrefix(path, prefix+"/")
}

// parseExecCommandWords splits a shell command into words using go-shellwords,
// handling quoted strings correctly. The command is first segmented by shell
// operators (;|&<>) to avoid cross-segment quoting confusion.
func parseExecCommandWords(command string) []string {
	var words []string
	for _, segment := range splitExecCommandSegments(command) {
		parser := shellwords.NewParser()
		parser.ParseBacktick = false
		parser.ParseEnv = false

		segmentWords, err := parser.Parse(segment)
		if err != nil || len(segmentWords) == 0 {
			words = append(words, strings.Fields(segment)...)
			continue
		}
		words = append(words, segmentWords...)
	}
	if len(words) == 0 {
		return strings.Fields(command)
	}
	return words
}

// splitExecCommandSegments splits a command string at shell operators (;|&<>)
// while respecting single and double quotes. Each segment can then be safely
// parsed by go-shellwords independently.
func splitExecCommandSegments(command string) []string {
	var segments []string
	start := 0
	inSingle := false
	inDouble := false

	for i := 0; i < len(command); i++ {
		ch := command[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			}
		case inDouble:
			if ch == '\\' && i+1 < len(command) {
				i++
			} else if ch == '"' {
				inDouble = false
			}
		default:
			switch ch {
			case '\\':
				if i+1 < len(command) {
					i++
				}
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case ';', '|', '&', '<', '>', '\n', '\r':
				if segment := strings.TrimSpace(command[start:i]); segment != "" {
					segments = append(segments, segment)
				}
				start = i + 1
			}
		}
	}

	if tail := strings.TrimSpace(command[start:]); tail != "" {
		segments = append(segments, tail)
	}
	return segments
}

// extractPathCandidates extracts potential file paths from a shell word,
// stripping prefixes like "file=@" or "--input=" to find the actual path.
func extractPathCandidates(word string) []string {
	if word == "" {
		return nil
	}

	queue := []string{word}
	seen := make(map[string]struct{}, 4)
	var out []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == "" {
			continue
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		if looksLikePathCandidate(current) {
			out = append(out, current)
		}
		for _, sep := range []string{"=", "@"} {
			if idx := strings.Index(current, sep); idx >= 0 && idx+1 < len(current) {
				queue = append(queue, current[idx+1:])
			}
		}
	}
	return out
}

// looksLikePathCandidate returns true if the string looks like a filesystem path.
func looksLikePathCandidate(s string) bool {
	if s == "" {
		return false
	}
	if filepath.IsAbs(s) {
		return true
	}
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, ".uploads/") ||
		strings.HasPrefix(s, ".goclaw/") ||
		strings.HasPrefix(s, "teams/") ||
		strings.HasPrefix(s, "tenants/") ||
		strings.HasPrefix(s, "~/") ||
		strings.Contains(s, "/") ||
		strings.Contains(s, `\`)
}

// canonicalizeExecPath resolves a path to its canonical absolute form,
// following symlinks where possible. Falls back to ancestor-based resolution
// for paths that don't fully exist yet.
func canonicalizeExecPath(path, baseDir string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(homeDir, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	absPath, _ := filepath.Abs(filepath.Clean(path))
	if real, err := filepath.EvalSymlinks(absPath); err == nil {
		return real, nil
	}
	return resolveThroughExistingAncestors(absPath)
}

// matchesAnyPathExemption checks if any path candidate extracted from a shell
// word matches any exemption after canonicalization. Rejects path traversal.
func matchesAnyPathExemption(word string, exemptions []string, baseDir string) bool {
	for _, candidate := range extractPathCandidates(word) {
		if strings.Contains(candidate, "..") {
			continue
		}
		realCandidate, err := canonicalizeExecPath(candidate, baseDir)
		if err != nil {
			continue
		}
		for _, exemption := range exemptions {
			realExemption, err := canonicalizeExecPath(exemption, baseDir)
			if err != nil {
				continue
			}
			if matchesPathExemption(realCandidate, []string{realExemption}) {
				return true
			}
		}
	}
	return false
}
