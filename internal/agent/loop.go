package agent

import (
	"log/slog"
	"strings"
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

// normalizeToolCall rewrites malformed MCP pseudo-calls that some models emit
// as `exec` with `{action:"mcp_xxx", code|command:"..."}`.
// We recover the intended MCP tool name from `action` and map payload to MCP
// schema (`code`) before registry lookup.
func (l *Loop) normalizeToolCall(tc providers.ToolCall) providers.ToolCall {
	if tc.Name != "exec" || len(tc.Arguments) == 0 {
		return tc
	}
	action, _ := tc.Arguments["action"].(string)
	if !strings.HasPrefix(action, "mcp_") {
		return tc
	}

	normalized := tc
	normalized.Name = action
	args := map[string]any{}
	if code, ok := tc.Arguments["code"]; ok {
		args["code"] = code
	} else if command, ok := tc.Arguments["command"]; ok {
		// Legacy prompt snippets sometimes place JS code in `command`.
		args["code"] = command
	}
	if len(args) == 0 {
		for k, v := range tc.Arguments {
			if k != "action" {
				args[k] = v
			}
		}
	}
	normalized.Arguments = args
	slog.Warn("tool call normalized from exec to mcp tool",
		"agent", l.id, "from", tc.Name, "to", normalized.Name)
	return normalized
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
