package nodehost

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// --- Shell script operand detection ---

func resolvePosixShellScriptOperandIndex(argv []string) *int {
	// If there's an inline command (-c), no script operand.
	m := ResolveInlineCommandMatch(argv, PosixInlineCommandFlags, true)
	if m.ValueTokenIndex != nil {
		return nil
	}
	afterDoubleDash := false
	for i := 1; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		if token == "-" {
			return nil
		}
		if !afterDoubleDash && token == "--" {
			afterDoubleDash = true
			continue
		}
		if !afterDoubleDash && token == "-s" {
			return nil
		}
		if !afterDoubleDash && strings.HasPrefix(token, "-") {
			flag := strings.SplitN(strings.ToLower(token), "=", 2)[0]
			if posixShellOptionsWithValue[flag] {
				if !strings.Contains(token, "=") {
					i++ // skip value
				}
				continue
			}
			continue
		}
		return intPtr(i)
	}
	return nil
}

// --- Bun/Deno script operand detection ---

func resolveBunScriptOperandIndex(argv []string, cwd string) *int {
	directIdx := resolveOptionFilteredPositionalIndex(argv, 1, bunOptionsWithValue)
	if directIdx == nil {
		return nil
	}
	directToken := strings.TrimSpace(safeIndex(argv, *directIdx))
	if directToken == "run" {
		return resolveOptionFilteredFileOperandIndex(argv, *directIdx+1, cwd, bunOptionsWithValue)
	}
	if bunSubcommands[directToken] {
		return nil
	}
	if !looksLikePathToken(directToken) {
		return nil
	}
	return directIdx
}

func resolveDenoRunScriptOperandIndex(argv []string, cwd string) *int {
	if strings.TrimSpace(safeIndex(argv, 1)) != "run" {
		return nil
	}
	return resolveOptionFilteredFileOperandIndex(argv, 2, cwd, denoRunOptionsWithValue)
}

// --- Generic operand resolution helpers ---

func resolveOptionFilteredPositionalIndex(argv []string, startIndex int, optionsWithValue map[string]bool) *int {
	afterDoubleDash := false
	for i := startIndex; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		if afterDoubleDash {
			return intPtr(i)
		}
		if token == "--" {
			afterDoubleDash = true
			continue
		}
		if token == "-" {
			return nil
		}
		if strings.HasPrefix(token, "-") {
			if !strings.Contains(token, "=") && optionsWithValue[token] {
				i++
			}
			continue
		}
		return intPtr(i)
	}
	return nil
}

func resolveOptionFilteredFileOperandIndex(argv []string, startIndex int, cwd string, optionsWithValue map[string]bool) *int {
	afterDoubleDash := false
	for i := startIndex; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		if afterDoubleDash {
			if resolvesToExistingFile(token, cwd) {
				return intPtr(i)
			}
			return nil
		}
		if token == "--" {
			afterDoubleDash = true
			continue
		}
		if token == "-" {
			return nil
		}
		if strings.HasPrefix(token, "-") {
			if !strings.Contains(token, "=") && optionsWithValue[token] {
				i++
			}
			continue
		}
		if resolvesToExistingFile(token, cwd) {
			return intPtr(i)
		}
		return nil
	}
	return nil
}

// resolveGenericInterpreterScriptOperandIndex finds a single file operand for generic interpreters.
func resolveGenericInterpreterScriptOperandIndex(argv []string, cwd string, optionsWithFileValue map[string]bool) *int {
	hits, sawOptionValueFile := collectExistingFileOperandIndexes(argv, 1, cwd, optionsWithFileValue)
	if sawOptionValueFile {
		return nil
	}
	if len(hits) == 1 {
		return intPtr(hits[0])
	}
	return nil
}

func collectExistingFileOperandIndexes(argv []string, startIndex int, cwd string, optionsWithFileValue map[string]bool) ([]int, bool) {
	afterDoubleDash := false
	var hits []int
	for i := startIndex; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		if afterDoubleDash {
			if resolvesToExistingFile(token, cwd) {
				hits = append(hits, i)
			}
			continue
		}
		if token == "--" {
			afterDoubleDash = true
			continue
		}
		if token == "-" {
			return nil, false
		}
		if strings.HasPrefix(token, "-") {
			flag, inline, _ := strings.Cut(token, "=")
			flagLower := strings.ToLower(flag)
			if optionsWithFileValue[flagLower] {
				if inline != "" && resolvesToExistingFile(inline, cwd) {
					hits = append(hits, i)
					return hits, true
				}
				nextToken := strings.TrimSpace(safeIndex(argv, i+1))
				if inline == "" && nextToken != "" && resolvesToExistingFile(nextToken, cwd) {
					hits = append(hits, i+1)
					return hits, true
				}
			}
			continue
		}
		if resolvesToExistingFile(token, cwd) {
			hits = append(hits, i)
		}
	}
	return hits, false
}

// --- Unsafe flag detection ---

func hasRubyUnsafeFlag(argv []string) bool {
	afterDoubleDash := false
	for i := 1; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		if afterDoubleDash {
			return false
		}
		if token == "--" {
			afterDoubleDash = true
			continue
		}
		if token == "-I" || token == "-r" {
			return true
		}
		if strings.HasPrefix(token, "-I") || strings.HasPrefix(token, "-r") {
			return true
		}
		if rubyUnsafeFlags[strings.ToLower(token)] {
			return true
		}
	}
	return false
}

func hasPerlUnsafeFlag(argv []string) bool {
	afterDoubleDash := false
	for i := 1; i < len(argv); i++ {
		token := strings.TrimSpace(safeIndex(argv, i))
		if token == "" {
			continue
		}
		if afterDoubleDash {
			return false
		}
		if token == "--" {
			afterDoubleDash = true
			continue
		}
		if token == "-I" || token == "-M" || token == "-m" {
			return true
		}
		if strings.HasPrefix(token, "-I") || strings.HasPrefix(token, "-M") || strings.HasPrefix(token, "-m") {
			return true
		}
		if perlUnsafeFlags[token] {
			return true
		}
	}
	return false
}

// --- Filesystem helpers ---

func looksLikePathToken(token string) bool {
	return strings.HasPrefix(token, ".") ||
		strings.HasPrefix(token, "/") ||
		strings.HasPrefix(token, "\\") ||
		strings.Contains(token, "/") ||
		strings.Contains(token, "\\") ||
		filepath.Ext(token) != ""
}

func resolvesToExistingFile(rawOperand, cwd string) bool {
	if rawOperand == "" {
		return false
	}
	resolved := rawOperand
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func hashFileContents(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
