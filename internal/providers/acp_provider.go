package providers

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
)

// ACPProvider implements Provider by orchestrating ACP-compatible agent subprocesses.
// It delegates to a ProcessPool that manages agent lifecycle over JSON-RPC 2.0 stdio.
type ACPProvider struct {
	pool           *acp.ProcessPool
	bridge         *acp.ToolBridge
	name           string // provider name for registry lookup (default: "acp")
	defaultModel   string
	permMode       string // permission mode for tool bridge
	mcpBridgeAddr  string // GoClaw MCP bridge address (e.g. "127.0.0.1:18790")
	mcpBridgeToken string // GoClaw gateway token for bridge auth
	sessionMu      sync.Map // sessionKey → *sync.Mutex
}

// ACPOption configures an ACPProvider.
type ACPOption func(*ACPProvider)

// WithACPModel sets the default model/agent name.
func WithACPModel(model string) ACPOption {
	return func(p *ACPProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithACPName sets the provider name for registry lookup.
// Allows multiple ACP providers (e.g. "claude-code", "codex") to coexist.
func WithACPName(name string) ACPOption {
	return func(p *ACPProvider) {
		if name != "" {
			p.name = name
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

// WithACPMCPBridge injects a GoClaw MCP bridge server into ACP sessions.
// This gives the ACP agent access to GoClaw tools (use_skill, exec, etc.) via MCP.
func WithACPMCPBridge(gatewayAddr, gatewayToken string) ACPOption {
	return func(p *ACPProvider) {
		p.mcpBridgeAddr = gatewayAddr
		p.mcpBridgeToken = gatewayToken
	}
}

// NewACPProvider creates a provider that orchestrates ACP agents as subprocesses.
func NewACPProvider(binary string, args []string, workDir string, idleTTL time.Duration, denyPatterns []*regexp.Regexp, opts ...ACPOption) *ACPProvider {
	p := &ACPProvider{
		name:         "acp",
		defaultModel: "claude",
	}
	for _, opt := range opts {
		opt(p)
	}

	// Create tool bridge with workspace sandboxing, deny patterns, and permission mode
	var bridgeOpts []acp.ToolBridgeOption
	if len(denyPatterns) > 0 {
		bridgeOpts = append(bridgeOpts, acp.WithDenyPatterns(denyPatterns))
	}
	if p.permMode != "" {
		bridgeOpts = append(bridgeOpts, acp.WithPermMode(p.permMode))
	}
	p.bridge = acp.NewToolBridge(workDir, bridgeOpts...)

	// Create process pool with tool bridge wired in
	p.pool = acp.NewProcessPool(binary, args, workDir, idleTTL)
	p.pool.SetToolHandler(p.bridge.Handle)

	// Inject MCP bridge so ACP sessions can access GoClaw tools (use_skill, etc.)
	// Uses SSE transport at /mcp/bridge/sse (ACP adapter requires SSE, not streamable-http).
	if p.mcpBridgeAddr != "" {
		headers := []acp.MCPHeader{
			{Name: "Authorization", Value: "Bearer " + p.mcpBridgeToken},
		}
		p.pool.SetMCPServers([]acp.MCPServerEntry{
			{
				Name:    "goclaw-bridge",
				Type:    "sse",
				URL:     fmt.Sprintf("http://%s/mcp/bridge/sse", p.mcpBridgeAddr),
				Headers: headers,
			},
		})
	}

	return p
}

func (p *ACPProvider) Name() string         { return p.name }
func (p *ACPProvider) DefaultModel() string { return p.defaultModel }

// Chat sends a prompt and returns the complete response (non-streaming).
func (p *ACPProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	if sessionKey == "" {
		return nil, fmt.Errorf("acp: session_key required in options")
	}

	unlock := p.lockSession(sessionKey)
	defer unlock()

	proc, err := p.pool.GetOrSpawn(ctx, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("acp: spawn failed: %w", err)
	}

	content := extractACPContent(req)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	// Collect all text from session/update notifications.
	// Supports both Zed adapter format (agent_message_chunk) and generic ACP format.
	var buf strings.Builder
	promptResp, err := proc.Prompt(ctx, content, func(update acp.SessionUpdate) {
		// Zed adapter: sessionUpdate = "agent_message_chunk" with single content block
		if update.SessionUpdateType == "agent_message_chunk" && update.Content != nil && update.Content.Type == "text" {
			buf.WriteString(update.Content.Text)
			return
		}
		// Generic ACP: message with content array
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					buf.WriteString(block.Text)
				}
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("acp: prompt failed: %w", err)
	}

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
		return nil, fmt.Errorf("acp: session_key required in options")
	}

	unlock := p.lockSession(sessionKey)
	defer unlock()

	proc, err := p.pool.GetOrSpawn(ctx, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("acp: spawn failed: %w", err)
	}

	content := extractACPContent(req)
	if len(content) == 0 {
		return nil, fmt.Errorf("acp: no user message in request")
	}

	// Handle context cancellation → send session/cancel
	cancelCtx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	go func() {
		<-cancelCtx.Done()
		if ctx.Err() == context.Canceled {
			_ = proc.Cancel()
		}
	}()

	var buf strings.Builder
	promptResp, err := proc.Prompt(ctx, content, func(update acp.SessionUpdate) {
		// Zed adapter: sessionUpdate = "agent_message_chunk" with single content block
		if update.SessionUpdateType == "agent_message_chunk" && update.Content != nil && update.Content.Type == "text" {
			onChunk(StreamChunk{Content: update.Content.Text})
			buf.WriteString(update.Content.Text)
			return
		}
		// Generic ACP: message with content array
		if update.Message != nil {
			for _, block := range update.Message.Content {
				if block.Type == "text" {
					onChunk(StreamChunk{Content: block.Text})
					buf.WriteString(block.Text)
				}
			}
		}
		if update.ToolCall != nil && update.ToolCall.Status == "running" {
			slog.Debug("acp: tool call", "name", update.ToolCall.Name)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("acp: prompt failed: %w", err)
	}

	onChunk(StreamChunk{Done: true})

	return &ChatResponse{
		Content:      buf.String(),
		FinishReason: mapStopReason(promptResp),
		Usage:        &Usage{},
	}, nil
}

// Close shuts down all subprocesses and cleans up terminals.
func (p *ACPProvider) Close() error {
	_ = p.bridge.Close()
	return p.pool.Close()
}

// lockSession acquires a per-session mutex (same pattern as ClaudeCLIProvider).
func (p *ACPProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// extractACPContent extracts user message + images from ChatRequest into ACP ContentBlocks.
func extractACPContent(req ChatRequest) []acp.ContentBlock {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	if userMsg == "" {
		return nil
	}

	var blocks []acp.ContentBlock

	// Prepend system prompt to first user message (ACP agents don't have separate system prompt API)
	text := userMsg
	if systemPrompt != "" {
		text = systemPrompt + "\n\n" + userMsg
	}
	blocks = append(blocks, acp.ContentBlock{Type: "text", Text: text})

	// Add images
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
	case "maxContextLength":
		return "length"
	default:
		return "stop"
	}
}
