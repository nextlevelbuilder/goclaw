package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
)

// acpSessionEntry tracks a live ACP session for one goclaw conversation.
type acpSessionEntry struct {
	id       string       // ACP session ID returned by session/new or session/load
	proc     *acp.ACPProcess // process that owns this session (for respawn detection)
	lastUsed time.Time
}

// ACPProvider implements Provider by orchestrating ACP-compatible agent subprocesses.
// One shared Gemini process is used; each goclaw conversation gets its own ACP session.
type ACPProvider struct {
	name            string
	binary          string // base binary name (claude / gemini / codex / ...) for vendor-specific behavior
	pool            *acp.ProcessPool
	bridge          *acp.ToolBridge
	defaultModel    string
	permMode        string
	poolKey         string // key for the shared process in the pool (binary + args)
	mcpServersFn    func(context.Context) []acp.McpServer // resolved per session
	includeDirs     []string                              // candidate dirs appended as --include-directories for gemini
	contextFileName string                                // e.g. "GEMINI.md", "CLAUDE.md", "AGENTS.md"; empty = no out-of-band system prompt

	acpSessions sync.Map // goclawSessionKey → *acpSessionEntry
	sessionMu   sync.Map // goclawSessionKey → *sync.Mutex (prevents concurrent session creation)

	done      chan struct{}
	closeOnce sync.Once
}

// contextFileNameForBinary returns the per-binary out-of-band system-prompt
// filename that the agent CLI auto-loads from its session cwd. Returns "" for
// unknown binaries — callers should fall back to in-prompt system instructions.
//
//   - claude → CLAUDE.md (Claude CLI native)
//   - gemini → GEMINI.md (Gemini CLI native)
//   - codex  → AGENTS.md (Codex agent file convention)
func contextFileNameForBinary(binary string) string {
	switch filepath.Base(binary) {
	case "claude":
		return "CLAUDE.md"
	case "gemini":
		return "GEMINI.md"
	case "codex":
		return "AGENTS.md"
	}
	return ""
}

// ACPOption configures an ACPProvider.
type ACPOption func(*ACPProvider)

// WithACPName overrides the provider name (default: "acp").
func WithACPName(name string) ACPOption {
	return func(p *ACPProvider) {
		if name != "" {
			p.name = name
		}
	}
}

// WithACPModel sets the default model/agent name.
func WithACPModel(model string) ACPOption {
	return func(p *ACPProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithACPPermMode sets the permission mode for the tool bridge.
func WithACPPermMode(mode string) ACPOption {
	return func(p *ACPProvider) {
		if mode != "" {
			p.permMode = mode
		}
	}
}

// WithACPMcpServersFunc registers a callback that returns the MCP server list
// to send on every session/new and session/load request. The callback receives
// the request context so it can resolve per-agent servers (e.g. from the MCP
// store based on agent ID in ctx). Return nil or an empty slice for no servers.
func WithACPMcpServersFunc(fn func(context.Context) []acp.McpServer) ACPOption {
	return func(p *ACPProvider) {
		p.mcpServersFn = fn
	}
}

// WithIncludeDirectories registers candidate directories that should be exposed
// to the agent's filesystem sandbox. The actual binary gating happens in
// NewACPProvider, which only emits `--include-directories <dir>` pairs for
// gemini and stat-filters non-existent entries. Storing the list on the
// provider for non-gemini binaries is harmless (never consumed downstream).
func WithIncludeDirectories(dirs []string) ACPOption {
	return func(p *ACPProvider) {
		p.includeDirs = dirs
	}
}

// NewACPProvider creates a provider that orchestrates ACP agents as subprocesses.
func NewACPProvider(binary string, args []string, workDir string, idleTTL time.Duration, denyPatterns []*regexp.Regexp, opts ...ACPOption) *ACPProvider {
	p := &ACPProvider{
		name:            "acp",
		binary:          binary,
		defaultModel:    "claude",
		contextFileName: contextFileNameForBinary(binary),
		done:            make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}

	// Gemini sandbox needs --include-directories to read goclaw skill paths
	// outside the cwd. Non-gemini binaries (claude, codex) handle filesystem
	// access differently, so includeDirs is a no-op for them.
	if filepath.Base(binary) == "gemini" && len(p.includeDirs) > 0 {
		for _, d := range p.includeDirs {
			if d == "" {
				continue
			}
			if info, err := os.Stat(d); err == nil && info.IsDir() {
				args = append(args, "--include-directories", d)
			}
		}
	}

	// Apply vendor-specific default args that goclaw's deployment model
	// requires for an ACP binary to function correctly inside our sandbox.
	args = applyVendorDefaultArgs(binary, args)

	// Pool key identifies the shared process: binary + final args combination.
	// Computed after args mutation so processes spawned with different
	// include-directories or vendor defaults are isolated correctly.
	p.poolKey = binary
	if len(args) > 0 {
		p.poolKey += "|" + strings.Join(args, " ")
	}

	var bridgeOpts []acp.ToolBridgeOption
	if len(denyPatterns) > 0 {
		bridgeOpts = append(bridgeOpts, acp.WithDenyPatterns(denyPatterns))
	}
	if p.permMode != "" {
		bridgeOpts = append(bridgeOpts, acp.WithPermMode(p.permMode))
	}
	p.bridge = acp.NewToolBridge(workDir, bridgeOpts...)

	p.pool = acp.NewProcessPool(binary, args, workDir, idleTTL)
	p.pool.SetToolHandler(p.bridge.Handle)
	if p.mcpServersFn != nil {
		p.pool.SetMcpServersFunc(p.mcpServersFn)
	}

	go p.sessionReaper()
	return p
}

// sessionReaper removes ACP sessions idle for more than 30 minutes.
// Sends session/cancel to release resources on the agent side before purging locally.
func (p *ACPProvider) sessionReaper() {
	const sessionIdleTTL = 30 * time.Minute
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.acpSessions.Range(func(key, value any) bool {
				entry := value.(*acpSessionEntry)
				if time.Since(entry.lastUsed) > sessionIdleTTL {
					slog.Info("acp: expiring idle session", "goclaw_session", key, "sid", entry.id)
					if entry.proc != nil {
						_ = entry.proc.Cancel(entry.id)
					}
					p.acpSessions.Delete(key)
					p.sessionMu.Delete(key)
				}
				return true
			})
		case <-p.done:
			return
		}
	}
}

// ensureSessionDir creates and returns a per-goclaw-session workspace under
// the process pool's base work directory. Mirrors the claude_cli provider's
// ensureWorkDir pattern so acp-workspaces layout matches cli-workspaces:
//
//	<baseWorkDir>/agent-<name>-ws-direct-<uuid>/
//
// Falls back to the pool's workDir (shared) if the base is unset or MkdirAll
// fails — safer than /tmp since the caller passes Authorization-protected
// paths to the ACP agent.
func (p *ACPProvider) ensureSessionDir(proc *acp.ACPProcess, goclawKey string) string {
	base := proc.WorkDir()
	if base == "" {
		return ""
	}
	safe := sanitizePathSegment(goclawKey)
	if safe == "" {
		return base
	}
	dir := filepath.Join(base, safe)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("acp: failed to create per-session workspace, using pool default",
			"goclaw_session", goclawKey, "dir", dir, "error", err)
		return base
	}
	return dir
}

// writeContextFile writes the system prompt to the per-binary auto-loaded
// context file in the session workspace (CLAUDE.md / GEMINI.md / AGENTS.md
// depending on the binary). The agent CLI reads this file automatically from
// the session cwd, so we don't have to repeat the system prompt on every turn.
//
// Skips entirely when the binary has no known context-file convention
// (p.contextFileName == ""): caller will then fall back to in-prompt system
// instructions via extractACPContent.
//
// Skips disk write when content is unchanged. Returns true only when the file
// was rewritten — that signals the caller to invalidate the live ACP session
// so the next request creates a fresh session that loads the updated file.
func (p *ACPProvider) writeContextFile(sessionDir, systemPrompt string) bool {
	if p.contextFileName == "" || sessionDir == "" || systemPrompt == "" {
		return false
	}
	path := filepath.Join(sessionDir, p.contextFileName)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return false
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("acp: failed to write context file", "path", path, "error", err)
		return false
	}
	return true
}

// resolveSession returns the ACP session ID for a goclaw session key.
// sessionDir is the pre-computed per-session workspace (caller must ensure it exists).
// Returns isNew=true only when a brand-new session is created via session/new —
// callers use this to inject full conversation history into the first prompt.
// A per-key mutex prevents concurrent creation races for the same session.
func (p *ACPProvider) resolveSession(ctx context.Context, proc *acp.ACPProcess, sessionDir, goclawKey string) (sid string, isNew bool, err error) {
	actual, _ := p.sessionMu.LoadOrStore(goclawKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if val, ok := p.acpSessions.Load(goclawKey); ok {
		entry := val.(*acpSessionEntry)
		if entry.proc == proc {
			// Same process instance: session is still live, just update last-used
			entry.lastUsed = time.Now()
			return entry.id, false, nil
		}
		// Process was respawned — try to restore the session
		slog.Info("acp: process respawned, attempting session restore",
			"goclaw_session", goclawKey, "old_sid", entry.id)
		if proc.AgentCaps().LoadSession {
			sid, err := proc.LoadSession(ctx, entry.id, sessionDir)
			// Some agents (notably Gemini CLI variants) return success with an
			// empty sessionId when session/load can't actually restore — that
			// would leave the prompt to fail with JSON-RPC -32603. Treat empty
			// as a soft failure and fall through to NewSession.
			if err == nil && sid != "" {
				p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now()})
				return sid, false, nil
			}
			if err != nil {
				slog.Warn("acp: session/load failed, creating new session", "old_sid", entry.id, "error", err)
			} else {
				slog.Warn("acp: session/load returned empty sid, creating new session", "old_sid", entry.id)
			}
		}
		// session/load not supported or failed — fall through to create new
	}

	slog.Info("acp: creating new session", "goclaw_session", goclawKey, "pool_key", p.poolKey, "cwd", sessionDir)
	sid, err = proc.NewSession(ctx, sessionDir)
	if err != nil {
		return "", false, err
	}
	p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now()})
	return sid, true, nil
}

func (p *ACPProvider) Name() string         { return p.name }
func (p *ACPProvider) DefaultModel() string { return p.defaultModel }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *ACPProvider) Capabilities() ProviderCapabilities {
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

// Chat sends a prompt and returns the complete response (non-streaming).
func (p *ACPProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("temp-%d", time.Now().UnixNano())
	}

	proc, err := p.pool.GetOrSpawn(ctx, p.poolKey)
	if err != nil {
		return nil, fmt.Errorf("acp: spawn failed: %w", err)
	}

	sessionDir := p.ensureSessionDir(proc, sessionKey)
	systemPrompt, _, _ := extractFromMessages(req.Messages)
	hasContextFile := p.contextFileName != ""
	if p.writeContextFile(sessionDir, systemPrompt) {
		// System prompt changed — invalidate live session so next resolveSession
		// creates a fresh one that loads the updated context file.
		p.acpSessions.Delete(sessionKey)
	}

	acpSessionID, isNew, err := p.resolveSession(ctx, proc, sessionDir, sessionKey)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req, isNew, hasContextFile)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	ctx = acp.WithGoclawSession(ctx, sessionKey)

	var buf strings.Builder
	var updateCount int
	promptResp, err := proc.Prompt(ctx, acpSessionID, content, func(update acp.SessionUpdate) {
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					buf.WriteString(block.Text)
					updateCount++
				}
			}
		}
	})
	if err != nil {
		slog.Error("acp: chat error", "session", sessionKey, "sid", acpSessionID, "error", err)
		return &ChatResponse{
			Content:      fmt.Sprintf("[ACP Error] %v", err),
			FinishReason: "error",
		}, err
	}

	if promptResp != nil && promptResp.StopReason == "cancelled" {
		slog.Warn("acp: chat cancelled", "session", sessionKey, "sid", acpSessionID, "updates", updateCount)
		errMsg := "[요청 취소] 응답 대기 중 타임아웃으로 취소됨"
		if buf.Len() > 0 {
			errMsg = buf.String() + "\n\n" + errMsg
		}
		return &ChatResponse{Content: errMsg, FinishReason: "stop"}, nil
	}

	slog.Info("acp: chat completed", "session", sessionKey, "sid", acpSessionID,
		"stopReason", mapStopReason(promptResp), "updates", updateCount, "contentLen", buf.Len())
	return &ChatResponse{
		Content:      buf.String(),
		FinishReason: mapStopReason(promptResp),
		Usage:        &Usage{},
	}, nil
}

// ChatStream sends a prompt and streams response chunks via onChunk callback.
func (p *ACPProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("temp-%d", time.Now().UnixNano())
	}

	proc, err := p.pool.GetOrSpawn(ctx, p.poolKey)
	if err != nil {
		return nil, fmt.Errorf("acp: spawn failed: %w", err)
	}

	sessionDir := p.ensureSessionDir(proc, sessionKey)
	systemPrompt, _, _ := extractFromMessages(req.Messages)
	hasContextFile := p.contextFileName != ""
	if p.writeContextFile(sessionDir, systemPrompt) {
		p.acpSessions.Delete(sessionKey)
	}

	acpSessionID, isNew, err := p.resolveSession(ctx, proc, sessionDir, sessionKey)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req, isNew, hasContextFile)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	ctx = acp.WithGoclawSession(ctx, sessionKey)

	// done channel ensures the cancel goroutine exits cleanly on normal completion,
	// preventing it from sending a spurious session/cancel after the prompt finishes.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				_ = proc.Cancel(acpSessionID)
			}
		case <-done:
		}
	}()

	var buf strings.Builder
	var updateCount int
	promptResp, err := proc.Prompt(ctx, acpSessionID, content, func(update acp.SessionUpdate) {
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					onChunk(StreamChunk{Content: block.Text})
					buf.WriteString(block.Text)
					updateCount++
				}
			}
		}
		if update.ToolCall != nil && update.ToolCall.Status == "running" {
			slog.Debug("acp: tool call", "name", update.ToolCall.Name)
		}
	})
	if err != nil {
		slog.Error("acp: chat error", "session", sessionKey, "sid", acpSessionID, "error", err)
		return &ChatResponse{
			Content:      fmt.Sprintf("[ACP Error] %v", err),
			FinishReason: "error",
		}, err
	}

	if promptResp != nil && promptResp.StopReason == "cancelled" {
		slog.Warn("acp: chat stream cancelled", "session", sessionKey, "sid", acpSessionID, "updates", updateCount)
		errMsg := "[요청 취소] 응답 대기 중 타임아웃으로 취소됨"
		prefix := "\n\n"
		if buf.Len() == 0 {
			prefix = ""
		}
		onChunk(StreamChunk{Content: prefix + errMsg})
		onChunk(StreamChunk{Done: true})
		return &ChatResponse{Content: buf.String() + prefix + errMsg, FinishReason: "stop"}, nil
	}

	onChunk(StreamChunk{Done: true})
	slog.Info("acp: chat stream completed", "session", sessionKey, "sid", acpSessionID,
		"stopReason", mapStopReason(promptResp), "updates", updateCount, "contentLen", buf.Len())

	return &ChatResponse{
		Content:      buf.String(),
		FinishReason: mapStopReason(promptResp),
		Usage:        &Usage{},
	}, nil
}

// purgeSession removes a session entry from both tracking maps.
// Sends session/cancel to release resources on the agent side before purging locally.
// Used to immediately discard one-shot (temp-) sessions after completion.
func (p *ACPProvider) purgeSession(key string) {
	if val, ok := p.acpSessions.Load(key); ok {
		entry := val.(*acpSessionEntry)
		if entry.proc != nil {
			_ = entry.proc.Cancel(entry.id)
		}
	}
	p.acpSessions.Delete(key)
	p.sessionMu.Delete(key)
	slog.Info("acp: purged temp session", "goclaw_session", key)
}

// Close shuts down all subprocesses and cleans up terminals.
func (p *ACPProvider) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
	})
	_ = p.bridge.Close()
	return p.pool.Close()
}

// extractACPContent builds ACP ContentBlocks from a ChatRequest.
//
// hasContextFile gates the optimised paths: it must be true only when
// writeContextFile actually produced a per-session CLAUDE.md / GEMINI.md /
// AGENTS.md that the agent CLI auto-loads as system prompt.
//
//   - hasContextFile=true,  isNew=false: only the current user message
//     (system prompt comes from the context file; agent retains history).
//   - hasContextFile=true,  isNew=true:  conversation transcript without
//     system role (context file already holds the system prompt; transcript
//     restores compacted summaries + recent turns after a fresh / reset
//     session).
//   - hasContextFile=false (unknown binary, no out-of-band context): include
//     the system prompt as a [System] block in front of the current user
//     message every turn. Conversation history is left to the agent's own
//     session state — this preserves pre-feature behaviour for binaries we
//     don't have a CLAUDE.md/GEMINI.md/AGENTS.md mapping for.
func extractACPContent(req ChatRequest, isNew, hasContextFile bool) []acp.ContentBlock {
	msgs := req.Messages

	if !hasContextFile {
		// Unknown binary: prepend system prompt to the current user message
		// every turn so the agent always sees its instructions, regardless of
		// what its own session-state machinery does or doesn't do.
		systemPrompt, userMsg, images := extractFromMessages(msgs)
		if userMsg == "" {
			return nil
		}
		text := userMsg
		if systemPrompt != "" {
			text = "[System]\n" + systemPrompt + "\n\n[User]\n" + userMsg
		}
		blocks := []acp.ContentBlock{{Type: "text", Text: text}}
		for _, img := range images {
			blocks = append(blocks, acp.ContentBlock{Type: "image", Data: img.Data, MimeType: img.MimeType})
		}
		return blocks
	}

	if !isNew {
		// Normal turn with context file: send only the current user message.
		_, userMsg, images := extractFromMessages(msgs)
		if userMsg == "" {
			return nil
		}
		blocks := []acp.ContentBlock{{Type: "text", Text: userMsg}}
		for _, img := range images {
			blocks = append(blocks, acp.ContentBlock{Type: "image", Data: img.Data, MimeType: img.MimeType})
		}
		return blocks
	}

	// New session: serialise full conversation context (summary + history + current).
	// System prompt is excluded — context file handles it.
	var sb strings.Builder
	var images []ImageContent
	for i, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "user":
			if i == len(msgs)-1 {
				images = m.Images // collect images from last (current) user message
			}
			sb.WriteString("[User]\n")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("[Assistant]\n")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		}
	}

	text := strings.TrimRight(sb.String(), "\n")
	if text == "" {
		return nil
	}
	blocks := []acp.ContentBlock{{Type: "text", Text: text}}
	for _, img := range images {
		blocks = append(blocks, acp.ContentBlock{Type: "image", Data: img.Data, MimeType: img.MimeType})
	}
	return blocks
}

// mapStopReason converts ACP stopReason to GoClaw finish reason.
func mapStopReason(resp *acp.PromptResponse) string {
	if resp == nil {
		return "stop"
	}
	switch resp.StopReason {
	case "max_tokens", "maxContextLength":
		return "length"
	case "cancelled":
		return "stop"
	default:
		return "stop"
	}
}

// applyVendorDefaultArgs appends vendor-specific CLI flags that goclaw's
// deployment model requires for the binary to behave correctly in ACP mode.
// Each entry is appended unconditionally when goclaw spawns the binary, so
// callers should not rely on the user's shell config or per-folder state.
//
// Current rules (keyed by filepath.Base of the binary path):
//
//   - gemini: append "--skip-trust" so MCP discovery runs even when the
//     per-session cwd lives under an untrusted parent in
//     ~/.gemini/trustedFolders.json. ACP sessions always run inside a
//     goclaw-managed sandbox, so the user-facing trust gate is moot here.
//
// Add new vendor entries here rather than scattering binary-name checks
// across the call sites.
func applyVendorDefaultArgs(binary string, args []string) []string {
	switch filepath.Base(binary) {
	case "gemini":
		return append(args, "--skip-trust")
	}
	return args
}
