package providers

import "sync"

// CursorCLIProvider implements Provider by shelling out to the Cursor `agent` binary.
// Auth via CURSOR_API_KEY env var injection per-call.
// Sessions tracked via workdir/.cursor_session_id (server-assigned chat IDs).
type CursorCLIProvider struct {
	cliPath       string // path to agent binary (default: "agent")
	defaultModel  string // default: "cursor-fast"
	apiKey        string // injected as CURSOR_API_KEY per-call
	baseWorkDir   string // base dir for per-session workspaces
	mcpConfigData *MCPConfigData // per-session MCP config data
	mu            sync.Mutex    // protects workdir creation
	sessionMu     sync.Map   // key: string, value: *sync.Mutex — per-session lock
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

// WithCursorCLIAPIKey sets the Cursor API key injected per-call.
func WithCursorCLIAPIKey(key string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		p.apiKey = key
	}
}

// WithCursorCLIMCPConfigData sets the per-session MCP config data.
func WithCursorCLIMCPConfigData(data *MCPConfigData) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		p.mcpConfigData = data
	}
}

// NewCursorCLIProvider creates a provider that invokes the Cursor agent CLI.
func NewCursorCLIProvider(cliPath string, opts ...CursorCLIOption) *CursorCLIProvider {
	if cliPath == "" {
		cliPath = "agent"
	}
	p := &CursorCLIProvider{
		cliPath:      cliPath,
		defaultModel: "cursor-fast",
		baseWorkDir:  defaultCursorCLIWorkDir(),
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
