package bootstrap

import (
	"fmt"
	"strings"
)

// Truncation constants matching TS pi-embedded-helpers/bootstrap.ts.
const (
	DefaultMaxCharsPerFile = 20_000 // per-file max before truncation
	DefaultTotalMaxChars   = 24_000 // total budget across all files
	MinFileBudget          = 64     // skip files if remaining budget below this
	HeadRatio              = 0.7    // keep 70% from beginning
	TailRatio              = 0.2    // keep 20% from end
)

// TruncateConfig controls truncation behavior.
type TruncateConfig struct {
	MaxCharsPerFile int // per-file max (default 20000)
	TotalMaxChars   int // total budget (default 24000)
}

// DefaultTruncateConfig returns the default truncation config.
func DefaultTruncateConfig() TruncateConfig {
	return TruncateConfig{
		MaxCharsPerFile: DefaultMaxCharsPerFile,
		TotalMaxChars:   DefaultTotalMaxChars,
	}
}

// BuildContextFiles converts bootstrap files into truncated context files
// ready for system prompt injection. Matches TS buildBootstrapContextFiles().
//
// Files are processed in order, each consuming from a shared total budget.
// Missing files are skipped. Large files are truncated with head/tail split.
func BuildContextFiles(files []File, cfg TruncateConfig) []ContextFile {
	if cfg.MaxCharsPerFile <= 0 {
		cfg.MaxCharsPerFile = DefaultMaxCharsPerFile
	}
	if cfg.TotalMaxChars <= 0 {
		cfg.TotalMaxChars = DefaultTotalMaxChars
	}

	remaining := cfg.TotalMaxChars
	var result []ContextFile

	for _, f := range files {
		if remaining < MinFileBudget {
			break
		}

		if f.Missing || strings.TrimSpace(f.Content) == "" {
			continue
		}

		// Truncate per-file
		content := trimContent(f.Content, f.Name, cfg.MaxCharsPerFile)

		// Clamp to remaining total budget
		content = clampToBudget(content, remaining)

		if content == "" {
			continue
		}

		result = append(result, ContextFile{
			Path:    f.Name,
			Content: content,
		})
		remaining -= len(content)
	}

	return result
}

// trimContent truncates file content with head/tail split if it exceeds maxChars.
// Matching TS trimBootstrapContent().
func trimContent(content, fileName string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}

	headChars := int(float64(maxChars) * HeadRatio)
	tailChars := int(float64(maxChars) * TailRatio)

	head := content[:headChars]
	tail := content[len(content)-tailChars:]

	marker := fmt.Sprintf(
		"\n\n[...truncated, read %s for full content...]\n...(%s: kept %d+%d chars of %d)...\n\n",
		fileName, fileName, headChars, tailChars, len(content),
	)

	return head + marker + tail
}

// clampToBudget truncates content to fit within the given character budget.
func clampToBudget(content string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if len(content) <= budget {
		return content
	}
	if budget <= 3 {
		return content[:budget]
	}
	return content[:budget-1] + "…"
}
