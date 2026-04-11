// Package pipeline — Sibling abort controller for parallel tool execution (CP-02).
// When an exec-family tool fails, cancel all sibling tools.
// Read-only tool failures are independent — don't cancel siblings.
package pipeline

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// SiblingAbortController manages cancellation of parallel tool siblings.
type SiblingAbortController struct {
	cancel  context.CancelFunc
	aborted atomic.Bool
}

// NewSiblingAbortController creates a child context with abort capability.
func NewSiblingAbortController(parent context.Context) (context.Context, *SiblingAbortController) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, &SiblingAbortController{cancel: cancel}
}

// ToolErrored is called when a parallel tool fails.
// Only exec-family tools trigger sibling cancellation.
// Read/search/fetch tools fail independently.
func (sac *SiblingAbortController) ToolErrored(toolName string, err error) {
	if !isExecFamilyTool(toolName) {
		slog.Debug("non-exec tool error — siblings continue",
			"tool", toolName, "err", err)
		return
	}

	if sac.aborted.CompareAndSwap(false, true) {
		slog.Warn("exec tool error — cancelling siblings",
			"tool", toolName, "err", err)
		sac.cancel()
	}
}

// IsAborted returns whether sibling abort has been triggered.
func (sac *SiblingAbortController) IsAborted() bool {
	return sac.aborted.Load()
}

func isExecFamilyTool(name string) bool {
	switch name {
	case "exec", "bash", "shell", "run_command", "run":
		return true
	}
	return false
}
