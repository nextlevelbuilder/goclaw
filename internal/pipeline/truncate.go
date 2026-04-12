// Package pipeline — Layer 1 of Context Defense (CP-01).
// Tool result truncation: oversized results persisted to disk, replaced with pointer.
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// TruncationConfig controls per-tool result size limits.
type TruncationConfig struct {
	// MaxResultChars is the maximum character count before truncation.
	// Default: 30000 (~7500 tokens).
	MaxResultChars int

	// OverflowDir is where full results are persisted.
	OverflowDir string

	// PreviewHeadChars and PreviewTailChars control the preview window.
	PreviewHeadChars int
	PreviewTailChars int
}

// DefaultTruncationConfig returns production defaults.
func DefaultTruncationConfig() TruncationConfig {
	return TruncationConfig{
		MaxResultChars:   30000,
		OverflowDir:      filepath.Join(os.TempDir(), "goclaw-overflow"),
		PreviewHeadChars: 500,
		PreviewTailChars: 500,
	}
}

// TruncationConfigForWorkspace stores overflow artifacts inside the active
// workspace so agents can read them back through read_file without tripping
// the path escape guard.
func TruncationConfigForWorkspace(workspacePath string) TruncationConfig {
	cfg := DefaultTruncationConfig()
	if workspacePath != "" {
		cfg.OverflowDir = filepath.Join(workspacePath, "overflow")
	}
	return cfg
}

// TruncateResult checks if content exceeds MaxResultChars.
// Returns (truncated, overflowRef). Fast path when under limit.
func TruncateResult(content string, cfg TruncationConfig) (string, string) {
	if len(content) <= cfg.MaxResultChars {
		return content, ""
	}

	ref := persistOverflow(content, cfg.OverflowDir)

	head := safeSlice(content, cfg.PreviewHeadChars)
	tail := safeSliceTail(content, cfg.PreviewTailChars)
	omitted := len(content) - len(head) - len(tail)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Truncated: %d chars. Full output: %s. Use read_file to access.]\n\n", len(content), ref))
	sb.WriteString(head)
	sb.WriteString(fmt.Sprintf("\n\n... (%d chars omitted) ...\n\n", omitted))
	sb.WriteString(tail)

	return sb.String(), ref
}

func persistOverflow(content string, dir string) string {
	_ = os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("overflow-%s.txt", uuid.New().String()[:8])
	path := filepath.Join(dir, name)
	_ = os.WriteFile(path, []byte(content), 0644)
	return path
}

func safeSlice(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func safeSliceTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
