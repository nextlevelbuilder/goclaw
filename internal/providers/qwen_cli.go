package providers

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// QwenCLIProvider implements Provider by shelling out to the `qwen` CLI binary.
// It acts as a thin proxy: CLI manages session history, tool execution, and context.
// GoClaw only forwards the latest user message and streams back the response.
type QwenCLIProvider struct {
	name         string     // provider name (default: "qwen-cli")
	cliPath      string     // path to qwen binary (default: "qwen")
	defaultModel string     // default: "coder-model"
	baseWorkDir  string     // base dir for agent workspaces
	approvalMode string     // approval mode (default: "yolo")
	mu           sync.Mutex // protects workdir creation
	sessionMu    sync.Map   // key: string, value: *sync.Mutex — per-session lock
}

// QwenCLIOption configures the provider.
type QwenCLIOption func(*QwenCLIProvider)

// WithQwenCLIName overrides the provider name (default: "qwen-cli").
func WithQwenCLIName(name string) QwenCLIOption {
	return func(p *QwenCLIProvider) {
		if name != "" {
			p.name = name
		}
	}
}

// WithQwenCLIModel sets the default model.
func WithQwenCLIModel(model string) QwenCLIOption {
	return func(p *QwenCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithQwenCLIWorkDir sets the base work directory.
func WithQwenCLIWorkDir(dir string) QwenCLIOption {
	return func(p *QwenCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

// WithQwenCLIApprovalMode sets the approval mode (plan, default, auto-edit, yolo).
func WithQwenCLIApprovalMode(mode string) QwenCLIOption {
	return func(p *QwenCLIProvider) {
		if mode != "" {
			p.approvalMode = mode
		}
	}
}

// NewQwenCLIProvider creates a provider that invokes the qwen CLI.
func NewQwenCLIProvider(cliPath string, opts ...QwenCLIOption) *QwenCLIProvider {
	if cliPath == "" {
		cliPath = "qwen"
	}
	p := &QwenCLIProvider{
		name:         "qwen-cli",
		cliPath:      cliPath,
		defaultModel: "coder-model",
		baseWorkDir:  defaultQwenCLIWorkDir(),
		approvalMode: "yolo",
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *QwenCLIProvider) Name() string         { return p.name }
func (p *QwenCLIProvider) DefaultModel() string { return p.defaultModel }

// ProviderType returns the provider type for routing/capability detection.
func (p *QwenCLIProvider) ProviderType() string { return "qwen_cli" }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *QwenCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: 128_000,
		TokenizerID:      "cl100k_base",
	}
}

// Close cleans up resources. Implements io.Closer.
func (p *QwenCLIProvider) Close() error {
	return nil
}

// lockSession acquires a per-session mutex to prevent concurrent CLI calls on the same session.
func (p *QwenCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// ensureWorkDir creates and returns a stable work directory for the given session key.
func (p *QwenCLIProvider) ensureWorkDir(sessionKey string) string {
	safe := sanitizePathSegment(sessionKey)
	dir := filepath.Join(p.baseWorkDir, safe)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("qwen-cli: failed to create workdir", "dir", dir, "error", err)
		return os.TempDir()
	}
	return dir
}

// writeQwenMD writes the system prompt to QWEN.md in the work directory.
func (p *QwenCLIProvider) writeQwenMD(workDir, systemPrompt string) {
	path := filepath.Join(workDir, "QWEN.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("qwen-cli: failed to write QWEN.md", "path", path, "error", err)
	}
}

// defaultQwenCLIWorkDir returns dataDir/qwen-cli-workspaces.
func defaultQwenCLIWorkDir() string {
	return filepath.Join(config.ResolvedDataDirFromEnv(), "qwen-cli-workspaces")
}
