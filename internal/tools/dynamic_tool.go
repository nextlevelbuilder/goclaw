package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DynamicTool wraps a CustomToolDef from the database and implements the Tool interface.
// Command templates use {{.key}} placeholders — all LLM-provided values are shell-escaped.
type DynamicTool struct {
	def       store.CustomToolDef
	workspace string
	params    map[string]any
}

// NewDynamicTool creates a Tool from a DB-stored custom tool definition.
func NewDynamicTool(def store.CustomToolDef, workspace string) *DynamicTool {
	var params map[string]any
	if len(def.Parameters) > 0 {
		json.Unmarshal(def.Parameters, &params)
	}
	if params == nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return &DynamicTool{def: def, workspace: workspace, params: params}
}

func (t *DynamicTool) Name() string               { return t.def.Name }
func (t *DynamicTool) Description() string        { return t.def.Description }
func (t *DynamicTool) Parameters() map[string]any { return t.params }

func (t *DynamicTool) Execute(ctx context.Context, args map[string]any) *Result {
	// Render command template with shell-escaped args
	command := renderCommand(t.def.Command, args)

	// Check deny patterns (same package — direct access to DefaultDenyPatterns)
	for _, pattern := range DefaultDenyPatterns {
		if pattern.MatchString(command) {
			return ErrorResult(fmt.Sprintf("command denied by safety policy: matches pattern %s", pattern.String()))
		}
	}

	// Timeout
	timeout := time.Duration(t.def.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Working directory — per-user workspace from context, fallback to tool's workspace.
	// Explicit WorkingDir on the tool definition still overrides everything.
	cwd := ToolWorkspaceFromCtx(ctx)
	if cwd == "" {
		cwd = t.workspace
	}
	if t.def.WorkingDir != "" {
		cwd = t.def.WorkingDir
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd

	// Decrypt and apply env vars
	if len(t.def.Env) > 0 {
		var envMap map[string]string
		if json.Unmarshal(t.def.Env, &envMap) == nil {
			for k, v := range envMap {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var result string
	if stdout.Len() > 0 {
		result = stdout.String()
	}
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += "STDERR:\n" + stderr.String()
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ErrorResult(fmt.Sprintf("command timed out after %s", timeout))
		}
		if result == "" {
			result = err.Error()
		}
		return ErrorResult(result)
	}

	if result == "" {
		result = "(command completed with no output)"
	}

	return SilentResult(result)
}

// renderCommand replaces {{.key}} placeholders with shell-escaped arg values.
// Uses simple string replacement (NOT Go text/template) for security.
func renderCommand(tmpl string, args map[string]any) string {
	result := tmpl
	for key, val := range args {
		placeholder := "{{." + key + "}}"
		escaped := shellEscape(fmt.Sprint(val))
		result = strings.ReplaceAll(result, placeholder, escaped)
	}
	return result
}

// shellEscape wraps a value in single quotes, escaping embedded single quotes.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
