package tools

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ClaudeRemoteTool runs Claude Code CLI on a remote workstation by composing a
// workstation_exec call. It does NOT re-implement MCP bridging — the remote CLI
// uses the workstation's local ~/.claude/ config (or the scoped CLAUDE_CONFIG_DIR).
//
// H2 fix: CLAUDE_CONFIG_DIR is scoped per session+agent hash to prevent concurrent
// agents from corrupting each other's ~/.claude/ auth tokens and session files.
//
// Permission enforcement is fully delegated to WorkstationExecTool.permCheck;
// ClaudeRemoteTool has no separate permission layer (Phase 6 covers both).
type ClaudeRemoteTool struct {
	inner *WorkstationExecTool
}

// NewClaudeRemoteTool creates a ClaudeRemoteTool backed by the given WorkstationExecTool.
func NewClaudeRemoteTool(exec *WorkstationExecTool) *ClaudeRemoteTool {
	return &ClaudeRemoteTool{inner: exec}
}

func (t *ClaudeRemoteTool) Name() string { return "claude_remote" }

func (t *ClaudeRemoteTool) Description() string {
	return "Run Claude Code CLI on a remote workstation. Requires Claude CLI installed and authenticated on the workstation. " +
		"Streams output as workstation.exec.chunk events."
}

func (t *ClaudeRemoteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Prompt to pass to Claude Code CLI via -p flag",
			},
			"workstation_id": map[string]any{
				"type":        "string",
				"description": "Workstation UUID or key (optional if agent has a default binding)",
			},
			"model": map[string]any{
				"type":        "string",
				"enum":        []string{"sonnet", "opus", "haiku"},
				"description": "Claude model alias to use (optional)",
			},
			"max_turns": map[string]any{
				"type":        "integer",
				"description": "Maximum agentic turns for Claude CLI (optional)",
			},
		},
		"required": []string{"prompt"},
	}
}

// Execute composes a `claude -p <prompt> --output-format stream-json` invocation
// and delegates to WorkstationExecTool.Execute. CLAUDE_CONFIG_DIR is injected
// per session+agent scope to prevent state contamination across concurrent agents.
func (t *ClaudeRemoteTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ErrorResult("prompt is required")
	}

	// Build claude CLI args.
	cmdArgs := []string{"-p", prompt, "--output-format", "stream-json"}

	if model, ok := args["model"].(string); ok && model != "" {
		cmdArgs = append(cmdArgs, "--model", model)
	}

	if maxTurns, ok := args["max_turns"].(float64); ok && maxTurns > 0 {
		cmdArgs = append(cmdArgs, "--max-turns", fmt.Sprintf("%d", int(maxTurns)))
	}

	// H2 fix: scope CLAUDE_CONFIG_DIR to session+agent to prevent cross-agent state corruption.
	// Uses first 12 hex chars of SHA-256(sessionKey+"-"+agentID) for a short, filesystem-safe path.
	sessionKey := ToolSessionKeyFromCtx(ctx)
	agentID := store.AgentIDFromContext(ctx).String()
	scopeInput := sessionKey + "-" + agentID
	rawHash := sha256.Sum256([]byte(scopeInput))
	scopeHash := fmt.Sprintf("%x", rawHash[:6]) // 6 bytes = 12 hex chars
	claudeConfigDir := "/tmp/goclaw-claude-" + scopeHash

	// Pass through to WorkstationExecTool with injected env and forwarded workstation_id.
	passthrough := map[string]any{
		"command": "claude",
		"args":    cmdArgs,
		"env": map[string]string{
			"CLAUDE_CONFIG_DIR": claudeConfigDir,
		},
		"timeout_sec": float64(600),
	}
	if wsID, ok := args["workstation_id"]; ok && wsID != nil {
		passthrough["workstation_id"] = wsID
	}

	return t.inner.Execute(ctx, passthrough)
}
