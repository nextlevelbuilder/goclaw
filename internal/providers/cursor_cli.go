package providers

import "sync"

// CursorCLIProvider implements Provider by shelling out to the Cursor `agent` binary.
// Authentication is via browser login (`agent login` on the server); credentials are not passed through GoClaw.
// Sessions tracked via workdir/.cursor_session_id (server-assigned chat IDs).
type CursorCLIProvider struct {
	cliPath       string         // path to agent binary (default: "agent")
	defaultModel  string         // default: "composer-2"
	baseWorkDir   string         // base dir for per-session workspaces
	permMode      string         // see WithCursorCLIPermMode / buildArgs
	mcpConfigData *MCPConfigData // per-session MCP config data
	mu            sync.Mutex     // protects workdir creation
	sessionMu     sync.Map       // key: string, value: *sync.Mutex — per-session lock
}

// CursorCLIOption configures the provider.
type CursorCLIOption func(*CursorCLIProvider)

// WithCursorCLIModel sets the default model.
func WithCursorCLIModel(model string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithCursorCLIWorkDir sets the base work directory.
func WithCursorCLIWorkDir(dir string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

// WithCursorCLIMCPConfigData sets the per-session MCP config data.
func WithCursorCLIMCPConfigData(data *MCPConfigData) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		p.mcpConfigData = data
	}
}

// WithCursorCLIPermMode sets how the Cursor `agent` subprocess handles permissions in --print mode.
// Values: "force" (default) — --force and --trust; "default" — --trust only (no --force); "sandbox" — force + trust + --sandbox enabled.
func WithCursorCLIPermMode(mode string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		if mode != "" {
			p.permMode = mode
		}
	}
}

// NewCursorCLIProvider creates a provider that invokes the Cursor agent CLI.
func NewCursorCLIProvider(cliPath string, opts ...CursorCLIOption) *CursorCLIProvider {
	if cliPath == "" {
		cliPath = "agent"
	}
	p := &CursorCLIProvider{
		cliPath:      cliPath,
		defaultModel: "composer-2",
		baseWorkDir:  defaultCursorCLIWorkDir(),
		permMode:     "force",
		// sessionMu is zero-value ready (sync.Map)
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *CursorCLIProvider) Name() string         { return "cursor-cli" }
func (p *CursorCLIProvider) DefaultModel() string { return p.defaultModel }

// Close is a no-op — Cursor workspaces persist for session continuity.
func (p *CursorCLIProvider) Close() error { return nil }

// lockSession acquires a per-session mutex to prevent concurrent CLI calls on the same session.
func (p *CursorCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
