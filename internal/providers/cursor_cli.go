package providers

import (
	"path/filepath"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// CursorCLIProvider implements Provider by shelling out to the Cursor `agent` CLI binary.
// It drives the headless Cursor Agent CLI to process prompts with streaming output.
type CursorCLIProvider struct {
	name         string // provider name (default: "cursor-cli")
	cliPath      string // path to agent binary (default: "agent")
	defaultModel string // default model alias
	defaultMode  string // default mode: "agent", "plan", "ask"
	baseWorkDir  string // base dir for agent workspaces
	mu           sync.Mutex
	sessionMu    sync.Map // key: string, value: *sync.Mutex
}

// cursorChatIDRegistries maps baseWorkDir -> *sync.Map (sessionKey -> cursorChatID).
// Package-level so ResetCursorCLISession can invalidate entries without a provider pointer.
var cursorChatIDRegistries sync.Map // key: string, value: *sync.Map

func getCursorChatIDRegistry(baseWorkDir string) *sync.Map {
	actual, _ := cursorChatIDRegistries.LoadOrStore(baseWorkDir, &sync.Map{})
	return actual.(*sync.Map)
}

// CursorCLIOption configures the provider.
type CursorCLIOption func(*CursorCLIProvider)

// WithCursorCLIName overrides the provider name.
func WithCursorCLIName(name string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		if name != "" {
			p.name = name
		}
	}
}

// WithCursorCLIModel sets the default model alias.
func WithCursorCLIModel(model string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithCursorCLIMode sets the default working mode.
func WithCursorCLIMode(mode string) CursorCLIOption {
	return func(p *CursorCLIProvider) {
		if mode != "" {
			p.defaultMode = mode
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

// NewCursorCLIProvider creates a provider that invokes the Cursor agent CLI.
func NewCursorCLIProvider(cliPath string, opts ...CursorCLIOption) *CursorCLIProvider {
	if cliPath == "" {
		cliPath = "agent"
	}
	p := &CursorCLIProvider{
		name:         "cursor-cli",
		cliPath:      cliPath,
		defaultModel: "",
		defaultMode:  "agent",
		baseWorkDir:  defaultCursorWorkDir(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *CursorCLIProvider) Name() string         { return p.name }
func (p *CursorCLIProvider) DefaultModel() string { return p.defaultModel }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *CursorCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         false,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: 200_000,
		TokenizerID:      "cl100k_base",
	}
}

// Close implements io.Closer (no-op for Cursor CLI — no temp files managed here).
func (p *CursorCLIProvider) Close() error {
	return nil
}

func defaultCursorWorkDir() string {
	return filepath.Join(config.ResolvedDataDirFromEnv(), "cursor-workspaces")
}

// lockSession acquires a per-session mutex to prevent concurrent CLI calls on the same session.
func (p *CursorCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
