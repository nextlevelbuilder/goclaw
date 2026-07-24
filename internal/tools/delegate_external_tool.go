package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DelegateExternalTool hands a task to one of the calling agent's CONNECTED
// external agents (Claude Code, Aider, …) — the specialists a user wires into
// an agent at creation time (agents.connected_agents). v1 supports the
// external_cli transport: run the connected agent's CLI inside the sandbox.
// MCP / A2A / HTTP transports are recognised but not yet dispatched.
type DelegateExternalTool struct {
	agents     store.AgentCRUDStore
	sandboxMgr sandbox.Manager
	workspace  string
	// Platform Anthropic credentials, used when a connection has no credential
	// of its own. Either may be empty; at least one is required to run Claude
	// Code. When both are set the OAuth token (subscription billing) is
	// preferred — see runCLI.
	anthropicKey        string // GOCLAW_ANTHROPIC_API_KEY → ANTHROPIC_API_KEY
	anthropicOAuthToken string // GOCLAW_ANTHROPIC_OAUTH_TOKEN → CLAUDE_CODE_OAUTH_TOKEN
}

// NewDelegateExternalTool wires the tool. sandboxMgr may be nil (no sandbox →
// external delegation returns a clear error rather than crashing).
func NewDelegateExternalTool(agents store.AgentCRUDStore, sandboxMgr sandbox.Manager, workspace, anthropicKey, anthropicOAuthToken string) *DelegateExternalTool {
	return &DelegateExternalTool{
		agents:              agents,
		sandboxMgr:          sandboxMgr,
		workspace:           workspace,
		anthropicKey:        anthropicKey,
		anthropicOAuthToken: anthropicOAuthToken,
	}
}

func (t *DelegateExternalTool) Name() string { return "delegate_external" }

func (t *DelegateExternalTool) Description() string {
	return "Hand a self-contained task to one of THIS agent's connected external agents — specialists wired into the agent at creation time (e.g. Claude Code for software engineering). Runs the connected agent and returns its result. Use it when the work squarely fits a connected specialist; otherwise do the work yourself."
}

func (t *DelegateExternalTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"connection": map[string]any{
				"type":        "string",
				"description": "Which connected agent to delegate to — its name (e.g. \"Claude Code\") or connection id. If the agent has exactly one connection, this may be omitted.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The task for the connected agent, stated as a complete, self-contained instruction (it does not see this conversation).",
			},
		},
		"required": []string{"task"},
	}
}

func (t *DelegateExternalTool) Execute(ctx context.Context, args map[string]any) *Result {
	task, _ := args["task"].(string)
	connArg, _ := args["connection"].(string)
	if strings.TrimSpace(task) == "" {
		return ErrorResult("task is required")
	}
	if t.sandboxMgr == nil {
		return ErrorResult("external delegation requires a sandbox, which is not configured on this deployment")
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("no calling agent in context — cannot resolve connected agents")
	}
	agent, err := t.agents.GetByID(ctx, agentID)
	if err != nil || agent == nil {
		return ErrorResult("could not load the calling agent's configuration")
	}
	conns := agent.ParseConnectedAgents()
	if len(conns) == 0 {
		return ErrorResult("this agent has no connected agents — add one on the agent's \"Connected agents\" section first")
	}
	conn := findConnection(conns, connArg)
	if conn == nil {
		names := make([]string, 0, len(conns))
		for i := range conns {
			names = append(names, conns[i].Name)
		}
		return ErrorResult(fmt.Sprintf("no connected agent matches %q — available: %s", connArg, strings.Join(names, ", ")))
	}

	switch conn.Kind {
	case "external_cli", "":
		return t.runCLI(ctx, conn, task)
	default:
		return ErrorResult(fmt.Sprintf("connected-agent transport %q is recognised but not dispatched yet (v1 supports external_cli)", conn.Kind))
	}
}

// findConnection matches by id, name, or provider (case-insensitive). If the
// agent has exactly one connection and no arg is given, that one is used.
func findConnection(conns []config.ConnectedAgentSpec, arg string) *config.ConnectedAgentSpec {
	arg = strings.TrimSpace(arg)
	if arg == "" && len(conns) == 1 {
		return &conns[0]
	}
	for i := range conns {
		c := &conns[i]
		if strings.EqualFold(c.ID, arg) || strings.EqualFold(c.Name, arg) || strings.EqualFold(c.Provider, arg) {
			return c
		}
	}
	if arg == "" && len(conns) == 1 {
		return &conns[0]
	}
	return nil
}

// runCLI runs a connected CLI agent inside the sandbox. Network is enabled ONLY
// for this exec (regular code exec stays --network none); the sandbox container
// is keyed separately so it never shares the network-isolated exec container.
func (t *DelegateExternalTool) runCLI(ctx context.Context, conn *config.ConnectedAgentSpec, task string) *Result {
	var command []string
	env := map[string]string{}

	switch strings.ToLower(conn.Provider) {
	case "claude_code", "claude", "claudecode":
		// Headless, auto-approve inside the isolated sandbox. Plain-text output.
		// NOTE: not --bare, so CLAUDE_CODE_OAUTH_TOKEN is honoured (bare mode
		// ignores it and requires ANTHROPIC_API_KEY).
		command = []string{"claude", "-p", task, "--permission-mode", "bypassPermissions", "--output-format", "text"}
		// Resolve exactly one credential and inject only its env var. The CLI's
		// own precedence has ANTHROPIC_API_KEY beating CLAUDE_CODE_OAUTH_TOKEN,
		// so setting both would make the choice implicit — we make it explicit.
		// Order: OAuth token (subscription billing) → API key (metered).
		// TODO(byok): a per-connection credential (conn.CredentialRef) resolved
		// here would win over both platform creds below.
		switch {
		case t.anthropicOAuthToken != "":
			env["CLAUDE_CODE_OAUTH_TOKEN"] = t.anthropicOAuthToken
		case t.anthropicKey != "":
			env["ANTHROPIC_API_KEY"] = t.anthropicKey
		default:
			return ErrorResult("Claude Code needs an Anthropic credential — set the platform subscription token (GOCLAW_ANTHROPIC_OAUTH_TOKEN), the platform API key (GOCLAW_ANTHROPIC_API_KEY), or a connection credential")
		}
	default:
		return ErrorResult(fmt.Sprintf("connected CLI provider %q is not available in the sandbox yet", conn.Provider))
	}

	// Network-enabled sandbox config for this exec only, dedicated container key.
	var netCfg sandbox.Config
	if cfg := SandboxConfigFromCtx(ctx); cfg != nil {
		netCfg = *cfg
	}
	netCfg.NetworkEnabled = true
	sandboxKey := "external:" + conn.ID

	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, &netCfg)
	if err != nil {
		slog.Warn("security.external_delegate_sandbox_unavailable", "provider", conn.Provider, "error", err)
		return ErrorResult(fmt.Sprintf("external delegation sandbox unavailable: %v", err))
	}
	result, err := sb.Exec(ctx, command, "", sandbox.WithEnv(env))
	if err != nil {
		return ErrorResult(fmt.Sprintf("connected agent %q failed to run: %v", conn.Name, err))
	}

	out := strings.TrimSpace(result.Stdout)
	if result.Stderr != "" && result.ExitCode != 0 {
		out += "\n[stderr]\n" + strings.TrimSpace(result.Stderr)
	}
	if out == "" {
		out = "(the connected agent produced no output)"
	}
	slog.Info("external delegation completed", "provider", conn.Provider, "connection", conn.ID, "exit", result.ExitCode)
	return NewResult(fmt.Sprintf("Delegated to %s (%s):\n\n%s", conn.Name, conn.Provider, out))
}
