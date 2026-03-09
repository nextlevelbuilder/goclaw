package providers

import (
	"log/slog"
	"sync"
)

// Options key for passing session key from agent loop to CLI provider.
const OptSessionKey = "session_key"

// OptDisableTools disables all built-in CLI tools when set to true.
// Useful for pure text generation (e.g. summoning) where tool use is unwanted.
const OptDisableTools = "disable_tools"

// ClaudeCLIProvider implements Provider by shelling out to the `claude` CLI binary.
// It acts as a thin proxy: CLI manages session history, tool execution, and context.
// GoClaw only forwards the latest user message and streams back the response.
type ClaudeCLIProvider struct {
	cliPath           string     // path to claude binary (default: "claude")
	defaultModel      string     // default: "sonnet"
	baseWorkDir       string     // base dir for agent workspaces
	mcpConfigPath     string     // pre-built MCP config file path (empty = no MCP)
	permMode          string     // permission mode (default: "bypassPermissions")
	hooksSettingsPath string     // generated settings.json with security hooks (empty = no hooks)
	hooksCleanup      func()     // cleanup function for hooks temp files
	mcpCleanup        func()     // cleanup function for MCP config temp file
	mu                sync.Mutex // protects workdir creation
	sessionMu         sync.Map   // key: string, value: *sync.Mutex — per-session lock
}

// ClaudeCLIOption configures the provider.
type ClaudeCLIOption func(*ClaudeCLIProvider)

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

// WithClaudeCLIMCPConfig sets the MCP config file path.
func WithClaudeCLIMCPConfig(path string, cleanup ...func()) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		p.mcpConfigPath = path
		if len(cleanup) > 0 && cleanup[0] != nil {
			p.mcpCleanup = cleanup[0]
		}
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

func (p *ClaudeCLIProvider) Name() string         { return "claude-cli" }
func (p *ClaudeCLIProvider) DefaultModel() string { return p.defaultModel }

// Close cleans up temp files (MCP config, hooks settings). Implements io.Closer.
func (p *ClaudeCLIProvider) Close() error {
	if p.mcpCleanup != nil {
		p.mcpCleanup()
	}
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
