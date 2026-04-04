package tools

import "strings"

// memoryHintPrefixes are shell commands that read/list/search filesystem content.
// If these target memory paths, the model should use dedicated memory tools instead.
var memoryHintPrefixes = []string{
	"cat ", "head ", "tail ", "less ", "more ",
	"grep ", "rg ", "ag ",
	"find ", "ls ", "wc ",
}

// memoryPathTokens are path fragments indicating memory file access.
var memoryPathTokens = []string{
	"MEMORY.md", "memory.md", "memory/",
}

// MaybeMemoryExecHint checks if a shell command targets memory files
// (which live in the database, not on disk) and returns an LLM hint.
// Returns empty string if no memory path detected.
func MaybeMemoryExecHint(command string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ""
	}

	// Check if command uses a filesystem read/list/search binary.
	hasReadCmd := false
	for _, prefix := range memoryHintPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			hasReadCmd = true
			break
		}
	}
	if !hasReadCmd {
		return ""
	}

	// Check if the command references a memory path.
	for _, token := range memoryPathTokens {
		if strings.Contains(trimmed, token) {
			return "[HINT] Memory files are in the database, not on disk. Use memory_search or memory_get tool instead."
		}
	}

	return ""
}
