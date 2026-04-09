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

// ACPProvider implements Provider by orchestrating ACP-compatible agent subprocesses.
// It delegates to a ProcessPool that manages agent lifecycle over JSON-RPC 2.0 stdio.
type ACPProvider struct {
	name         string // provider name (default: "acp")
	pool         *acp.ProcessPool
	bridge       *acp.ToolBridge
	defaultModel string
	permMode     string // permission mode for tool bridge
	sessionMu    sync.Map // sessionKey → *sync.Mutex
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

	// Collect all text from session/update notifications
	var buf strings.Builder
	promptResp, err := proc.Prompt(ctx, content, func(update acp.SessionUpdate) {
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
		if errors.Is(ctx.Err(), context.Canceled) {
			_ = proc.Cancel()
		}
	}()

	var buf strings.Builder
	promptResp, err := proc.Prompt(ctx, content, func(update acp.SessionUpdate) {
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
