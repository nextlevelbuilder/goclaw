package providers

import (
	"log/slog"
	"os"
	"sync"
)

// Options key for passing session key from agent loop to CLI provider.
const OptSessionKey = "session_key"

// OptDisableTools disables all built-in CLI tools when set to true.
// Useful for pure text generation (e.g. summoning) where tool use is unwanted.
const OptDisableTools = "disable_tools"

// OptAgentID passes the agent UUID string for per-session MCP config.
const OptAgentID = "agent_id"

// OptUserID passes the user ID string for per-session MCP config.
const OptUserID = "user_id"

// OptChannel passes the source channel (telegram, discord, etc.) for MCP bridge context.
const OptChannel = "channel"

// OptChatID passes the source chat ID for MCP bridge context.
const OptChatID = "chat_id"

// OptPeerKind passes the peer kind (direct/group) for MCP bridge context.
const OptPeerKind = "peer_kind"

// OptWorkspace passes the agent workspace path so MCP bridge tools can resolve file paths.
const OptWorkspace = "workspace"

// OptTenantID passes the tenant UUID string for per-session MCP config.
// Required for memory indexing and tenant-scoped queries via bridge tools.
const OptTenantID = "tenant_id"

// OptLocalKey passes the composite local key (e.g. "-100123:topic:42") for forum topic routing.
const OptLocalKey = "local_key"

// ClaudeCLIProvider implements Provider by shelling out to the `claude` CLI binary.
// It acts as a thin proxy: CLI manages session history, tool execution, and context.
// GoClaw only forwards the latest user message and streams back the response.
type ClaudeCLIProvider struct {
	name               string // provider name (default: "claude-cli")
	cliPath            string // path to claude binary (default: "claude")
	defaultModel       string // default: "sonnet"
	baseWorkDir        string // base dir for agent workspaces
	mcpConfigData      *MCPConfigData // per-session MCP config data
	permMode           string // permission mode (default: "bypassPermissions")
	hooksSettingsPath  string // generated settings.json with security hooks (empty = no hooks)
	hooksCleanup       func() // cleanup function for hooks temp files
	mu                 sync.Mutex // protects workdir creation
	sessionMu          sync.Map   // key: string, value: *sync.Mutex — per-session lock
	mcpConfigDirs      sync.Map   // key: string (dir path), value: struct{} — tracks per-session MCP config dirs for cleanup
}

// ClaudeCLIOption configures the provider.
type ClaudeCLIOption func(*ClaudeCLIProvider)

// WithClaudeCLIName overrides the provider name (default: "claude-cli").
func WithClaudeCLIName(name string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if name != "" {
			p.name = name
		}
	}
}

// WithClaudeCLIModel sets the default model alias.
func WithClaudeCLIModel(model string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithClaudeCLIWorkDir sets the base work directory.
func WithClaudeCLIWorkDir(dir string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

// WithClaudeCLIMCPConfigData sets the per-session MCP config data.
// Per-session configs are written on each Chat/ChatStream call with agent context.
func WithClaudeCLIMCPConfigData(data *MCPConfigData) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		p.mcpConfigData = data
	}
}

// WithClaudeCLIPermMode sets the permission mode.
func WithClaudeCLIPermMode(mode string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if mode != "" {
			p.permMode = mode
		}
	}
}

// WithClaudeCLISecurityHooks enables GoClaw security hooks for CLI tool calls.
// Generates a settings file with PreToolUse hooks that enforce shell deny patterns
// and workspace path restrictions.
func WithClaudeCLISecurityHooks(workspace string, restrictToWorkspace bool) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		settingsPath, cleanup, err := BuildCLIHooksConfig(workspace, restrictToWorkspace)
		if err != nil {
			slog.Warn("claude-cli: failed to build security hooks", "error", err)
			return
		}
		p.hooksSettingsPath = settingsPath
		p.hooksCleanup = cleanup
	}
}

// NewClaudeCLIProvider creates a provider that invokes the claude CLI.
func NewClaudeCLIProvider(cliPath string, opts ...ClaudeCLIOption) *ClaudeCLIProvider {
	if cliPath == "" {
		cliPath = "claude"
	}
	p := &ClaudeCLIProvider{
		name:         "claude-cli",
		cliPath:      cliPath,
		defaultModel: "sonnet",
		baseWorkDir:  defaultCLIWorkDir(),
		permMode:     "bypassPermissions",
		// sessionMu is zero-value ready (sync.Map)
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *ClaudeCLIProvider) Name() string        { return p.name }
func (p *ClaudeCLIProvider) DefaultModel() string { return p.defaultModel }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
// ClaudeCLI is subprocess-based — no HTTP adapter, capabilities only.
func (p *ClaudeCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: 200_000,
		TokenizerID:      "cl100k_base",
	}
}

// Close cleans up temp files (per-session MCP configs, hooks settings). Implements io.Closer.
func (p *ClaudeCLIProvider) Close() error {
	// Clean up per-session MCP config directories this provider created
	p.mcpConfigDirs.Range(func(key, _ any) bool {
		dir := key.(string)
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("claude-cli: failed to clean mcp config dir", "dir", dir, "error", err)
		}
		return true
	})
	if p.hooksCleanup != nil {
		p.hooksCleanup()
	}
	return nil
}

// lockSession acquires a per-session mutex to prevent concurrent CLI calls on the same session.
func (p *ClaudeCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
