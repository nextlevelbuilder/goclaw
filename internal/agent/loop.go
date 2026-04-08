package agent

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// runLoop (v2 agent iteration loop) was removed in the v3 force migration.
// All agents now use the v3 pipeline (runViaPipeline in loop_pipeline_adapter.go).
// Shared helpers below are still used by v3 pipeline callbacks.

// indexedResult holds the output of a single parallel tool execution, preserving
// the original call index so results can be sorted back into deterministic order.
type indexedResult struct {
	idx          int
	tc           providers.ToolCall
	registryName string
	result       *tools.Result
	argsJSON     string
	spanStart    time.Time
}

// resolveToolCallName strips the configured tool call prefix from a name
// returned by the model, returning the original registry name.
// Example: prefix "proxy_" + model calls "proxy_exec" → returns "exec".
func (l *Loop) resolveToolCallName(name string) string {
	if l.agentToolPolicy != nil && l.agentToolPolicy.ToolCallPrefix != "" {
		return tools.StripToolPrefix(l.agentToolPolicy.ToolCallPrefix, name)
	}
	return name
}

func hasParseErrors(calls []providers.ToolCall) bool {
	for _, tc := range calls {
		if tc.ParseError != "" {
			return true
		}
	}
	return false
}

func truncateToolArgs(args map[string]any, maxLen int) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > maxLen {
			out[k] = truncateStr(s, maxLen)
		} else {
			out[k] = v
		}
	}
	return out
}
