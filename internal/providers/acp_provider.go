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
	name           string
	pool           *acp.ProcessPool
	bridge         *acp.ToolBridge
	defaultModel   string
	permMode       string
	poolKey        string // key for the shared process in the pool (binary + args)
	mcpServersFn   func(context.Context) []acp.McpServer // resolved per session
	sessionIdleTTL time.Duration                         // idle TTL for ACP session reaper
	promptTimeout  time.Duration                         // inactivity timeout for Prompt() watchdog

	acpSessions sync.Map // goclawSessionKey → *acpSessionEntry
	sessionMu   sync.Map // goclawSessionKey → *sync.Mutex (prevents concurrent session creation)

	done      chan struct{}
	closeOnce sync.Once
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

// WithACPSessionTTL overrides the idle TTL used by the session reaper.
// When not set, defaults to the process pool's idleTTL.
func WithACPSessionTTL(d time.Duration) ACPOption {
	return func(p *ACPProvider) {
		if d > 0 {
			p.sessionIdleTTL = d
		}
	}
}

// WithACPPromptTimeout sets the inactivity timeout for Prompt() watchdogs.
// Overrides the package-level promptInactivityTimeout (10 min default).
func WithACPPromptTimeout(d time.Duration) ACPOption {
	return func(p *ACPProvider) {
		if d > 0 {
			p.promptTimeout = d
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

// NewACPProvider creates a provider that orchestrates ACP agents as subprocesses.
func NewACPProvider(binary string, args []string, workDir string, idleTTL time.Duration, denyPatterns []*regexp.Regexp, opts ...ACPOption) *ACPProvider {
	p := &ACPProvider{
		name:         "acp",
		defaultModel: "claude",
		done:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}

	// poolKey uniquely identifies a subprocess configuration so that providers
	// differing in any of the five dimensions always spawn separate processes.
	// permMode is included explicitly; it is no longer injected into CLI args
	// because ACP permission/request RPCs are handled entirely by ToolBridge.
	p.poolKey = fmt.Sprintf("%s|%s|%s|%s|%s",
		binary,
		strings.Join(args, " "),
		workDir,
		idleTTL,
		p.permMode,
	)

	if p.sessionIdleTTL == 0 {
		p.sessionIdleTTL = idleTTL
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
	if p.promptTimeout > 0 {
		p.pool.SetPromptTimeout(p.promptTimeout)
	}

	go p.sessionReaper()
	return p
}

// sessionReaper removes ACP sessions idle for more than sessionIdleTTL.
// Sends session/cancel to release resources on the agent side before purging locally.
func (p *ACPProvider) sessionReaper() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.acpSessions.Range(func(key, value any) bool {
				entry := value.(*acpSessionEntry)
				if time.Since(entry.lastUsed) > p.sessionIdleTTL {
					slog.Info("acp: expiring idle session", "goclaw_session", key, "sid", entry.id, "ttl", p.sessionIdleTTL)
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

// writeGeminiMD writes the system prompt to GEMINI.md in the session workspace.
// Gemini CLI reads this file automatically from the session cwd (mirrors writeClaudeMD).
// Skips write if content is unchanged. Returns true if the file was rewritten,
// signalling the caller to invalidate the live ACP session so the next request
// starts a fresh session with the updated instructions.
func (p *ACPProvider) writeGeminiMD(sessionDir, systemPrompt string) bool {
	if sessionDir == "" || systemPrompt == "" {
		return false
	}
	path := filepath.Join(sessionDir, "GEMINI.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return false
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("acp: failed to write GEMINI.md", "path", path, "error", err)
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
			if err == nil {
				p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now()})
				return sid, false, nil
			}
			slog.Warn("acp: session/load failed, creating new session", "old_sid", entry.id, "error", err)
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
	if p.writeGeminiMD(sessionDir, systemPrompt) {
		// System prompt changed — invalidate live session so next resolveSession
		// creates a fresh one that loads the updated GEMINI.md.
		p.acpSessions.Delete(sessionKey)
	}

	acpSessionID, isNew, err := p.resolveSession(ctx, proc, sessionDir, sessionKey)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req, isNew)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	ctx = acp.WithGoclawSession(ctx, sessionKey)

	var buf strings.Builder
	var updateCount int
	cb := func(update acp.SessionUpdate) {
		if update.ToolCall != nil {
			slog.Info("acp: tool call (chat)", "name", update.ToolCall.Name, "status", update.ToolCall.Status, "id", update.ToolCall.ID)
		}
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					buf.WriteString(block.Text)
					updateCount++
				}
			}
		}
	}

	const maxACPRetry = 2
	var promptResp *acp.PromptResponse
	for attempt := range maxACPRetry + 1 {
		buf.Reset()
		updateCount = 0
		promptResp, err = proc.Prompt(ctx, acpSessionID, content, cb)
		if err == nil || !isMalformedFunctionCall(err) {
			break
		}
		slog.Warn("acp: malformed function call, retrying", "attempt", attempt+1, "session", sessionKey, "sid", acpSessionID)
	}

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

	outputText := buf.String()
	slog.Info("acp: chat completed", "session", sessionKey, "sid", acpSessionID,
		"stopReason", mapStopReason(promptResp), "updates", updateCount, "contentLen", len(outputText))
	return &ChatResponse{
		Content:      outputText,
		FinishReason: mapStopReason(promptResp),
		Usage: &Usage{
			PromptTokens:     acpInputTokens(req.Messages),
			CompletionTokens: acpEstimateTokens(outputText),
			TotalTokens:      acpInputTokens(req.Messages) + acpEstimateTokens(outputText),
		},
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
	if p.writeGeminiMD(sessionDir, systemPrompt) {
		p.acpSessions.Delete(sessionKey)
	}

	acpSessionID, isNew, err := p.resolveSession(ctx, proc, sessionDir, sessionKey)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req, isNew)
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
	streamCb := func(update acp.SessionUpdate) {
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					onChunk(StreamChunk{Content: block.Text})
					buf.WriteString(block.Text)
					updateCount++
				}
			}
		}
		if update.ToolCall != nil {
			slog.Info("acp: tool call (stream)", "name", update.ToolCall.Name, "status", update.ToolCall.Status, "id", update.ToolCall.ID)
		}
	}

	const maxACPRetry = 2
	var promptResp *acp.PromptResponse
	for attempt := range maxACPRetry + 1 {
		promptResp, err = proc.Prompt(ctx, acpSessionID, content, streamCb)
		if err == nil || !isMalformedFunctionCall(err) {
			break
		}
		slog.Warn("acp: malformed function call, retrying", "attempt", attempt+1, "session", sessionKey, "sid", acpSessionID)
	}

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
	outputText := buf.String()
	slog.Info("acp: chat stream completed", "session", sessionKey, "sid", acpSessionID,
		"stopReason", mapStopReason(promptResp), "updates", updateCount, "contentLen", len(outputText))

	return &ChatResponse{
		Content:      outputText,
		FinishReason: mapStopReason(promptResp),
		Usage: &Usage{
			PromptTokens:     acpInputTokens(req.Messages),
			CompletionTokens: acpEstimateTokens(outputText),
			TotalTokens:      acpInputTokens(req.Messages) + acpEstimateTokens(outputText),
		},
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

// acpAllowedMIME is the set of image MIME types accepted by ACP providers.
var acpAllowedMIME = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// acpMaxImageBytes is the maximum decoded image size accepted (5 MB).
const acpMaxImageBytes = 5 * 1024 * 1024

// appendACPImages appends validated image ContentBlocks to blocks.
func appendACPImages(blocks []acp.ContentBlock, images []ImageContent) []acp.ContentBlock {
	for _, img := range images {
		if !acpAllowedMIME[img.MimeType] {
			slog.Warn("acp: unsupported image MIME type, skipping", "mime", img.MimeType)
			continue
		}
		if len(img.Data)*3/4 > acpMaxImageBytes {
			slog.Warn("acp: image too large, skipping", "estimatedBytes", len(img.Data)*3/4, "limit", acpMaxImageBytes)
			continue
		}
		blocks = append(blocks, acp.ContentBlock{Type: "image", Data: img.Data, MimeType: img.MimeType})
	}
	return blocks
}

// extractACPContent builds ACP ContentBlocks from a ChatRequest.
//
// isNew=false (normal turn): GEMINI.md in the session workspace already provides
// the system prompt, so only the current user message is sent. This avoids
// repeating the (often large) system prompt on every turn.
//
// isNew=true (fresh or reset session): the session has no prior context.
// All non-system messages from req.Messages are serialised as a conversation
// transcript so that compacted summaries and recent history are preserved.
// The system prompt is omitted here because writeGeminiMD wrote it to GEMINI.md
// before the session was created.
func extractACPContent(req ChatRequest, isNew bool) []acp.ContentBlock {
	msgs := req.Messages

	if !isNew {
		// Normal turn: send only the current user message.
		_, userMsg, images := extractFromMessages(msgs)
		if userMsg == "" {
			return nil
		}
		blocks := []acp.ContentBlock{{Type: "text", Text: userMsg}}
		return appendACPImages(blocks, images)
	}

	// New session: serialise full conversation context (summary + history + current).
	// System prompt is excluded — GEMINI.md handles it.
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
	return appendACPImages(blocks, images)
}

// mapStopReason converts ACP stopReason to GoClaw finish reason.
func mapStopReason(resp *acp.PromptResponse) string {
	if resp == nil {
		return "stop"
	}
	switch resp.StopReason {
	case "max_tokens", "maxContextLength":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "error":
		return "error"
	default: // end_turn, stop_sequence, cancelled, ""
		return "stop"
	}
}

// isMalformedFunctionCall returns true when err indicates Gemini produced an
// invalid tool call JSON — a transient model glitch worth retrying.
func isMalformedFunctionCall(err error) bool {
	return err != nil && strings.Contains(err.Error(), "malformed function call")
}

// acpEstimateTokens returns a rough token count from character count (chars/4).
func acpEstimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 && len(s) > 0 {
		return 1
	}
	return n
}

// acpInputTokens estimates input token count from all messages.
func acpInputTokens(msgs []Message) int {
	var total int
	for _, m := range msgs {
		total += acpEstimateTokens(m.Content)
	}
	return total
}
