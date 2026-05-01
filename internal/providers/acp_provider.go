package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	name         string
	pool         *acp.ProcessPool
	bridge       *acp.ToolBridge
	defaultModel string
	permMode     string
	poolKey      string // key for the shared process in the pool (binary + args)

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

// NewACPProvider creates a provider that orchestrates ACP agents as subprocesses.
func NewACPProvider(binary string, args []string, workDir string, idleTTL time.Duration, denyPatterns []*regexp.Regexp, opts ...ACPOption) *ACPProvider {
	// Pool key identifies the shared process: binary + args combination
	poolKey := binary
	if len(args) > 0 {
		poolKey += "|" + strings.Join(args, " ")
	}

	p := &ACPProvider{
		name:         "acp",
		defaultModel: "claude",
		poolKey:      poolKey,
		done:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
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
				}
				return true
			})
		case <-p.done:
			return
		}
	}
}

// resolveSession returns the ACP session ID for a goclaw session key.
// It creates a new session if none exists, or reloads it after a process respawn.
// A per-key mutex prevents concurrent creation races for the same session.
func (p *ACPProvider) resolveSession(ctx context.Context, proc *acp.ACPProcess, goclawKey string) (string, error) {
	actual, _ := p.sessionMu.LoadOrStore(goclawKey, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if val, ok := p.acpSessions.Load(goclawKey); ok {
		entry := val.(*acpSessionEntry)
		if entry.proc == proc {
			// Same process instance: session is still live, just update last-used
			entry.lastUsed = time.Now()
			return entry.id, nil
		}
		// Process was respawned — try to restore the session
		slog.Info("acp: process respawned, attempting session restore",
			"goclaw_session", goclawKey, "old_sid", entry.id)
		if proc.AgentCaps().LoadSession {
			sid, err := proc.LoadSession(ctx, entry.id)
			if err == nil {
				p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now()})
				return sid, nil
			}
			slog.Warn("acp: session/load failed, creating new session", "old_sid", entry.id, "error", err)
		}
		// session/load not supported or failed — fall through to create new
	}

	slog.Info("acp: creating new session", "goclaw_session", goclawKey, "pool_key", p.poolKey)
	sid, err := proc.NewSession(ctx)
	if err != nil {
		return "", err
	}
	p.acpSessions.Store(goclawKey, &acpSessionEntry{id: sid, proc: proc, lastUsed: time.Now()})
	return sid, nil
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

	acpSessionID, err := p.resolveSession(ctx, proc, sessionKey)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req)
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

	acpSessionID, err := p.resolveSession(ctx, proc, sessionKey)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(sessionKey, "temp-") {
		defer p.purgeSession(sessionKey)
	}

	content := extractACPContent(req)
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

// extractACPContent extracts user message + images from ChatRequest into ACP ContentBlocks.
func extractACPContent(req ChatRequest) []acp.ContentBlock {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	if userMsg == "" {
		return nil
	}

	var blocks []acp.ContentBlock

	// Prepend system prompt to user message (ACP agents have no separate system prompt API)
	text := userMsg
	if systemPrompt != "" {
		text = systemPrompt + "\n\n" + userMsg
	}
	blocks = append(blocks, acp.ContentBlock{Type: "text", Text: text})

	for _, img := range images {
		blocks = append(blocks, acp.ContentBlock{
			Type:     "image",
			Data:     img.Data,
			MimeType: img.MimeType,
		})
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
