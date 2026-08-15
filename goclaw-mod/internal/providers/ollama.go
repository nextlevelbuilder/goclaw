package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	ollamaapi "github.com/ollama/ollama/api"
)

// OllamaProvider implements Provider using the official Ollama Go client.
// It sends requests to the native /api/chat endpoint, which honors
// options.num_ctx, tool calls, and thinking (think parameter).
type OllamaProvider struct {
	name         string
	apiBase      string
	defaultModel string
	apiKey       string
	numCtx       *int
	client       *ollamaapi.Client
	retryConfig  RetryConfig

	// ctxCache memoises each model's context window as reported by /api/show,
	// keyed by model name. Resolution happens on the first request for a model
	// rather than at startup, because the model an agent uses is only known then.
	ctxMu    sync.RWMutex
	ctxCache map[string]int

	// thinkingEnabled is the provider-level override for whether requests
	// should ask Ollama to emit visible reasoning/thinking tokens.
	// nil = default off (see buildRequest).
	thinkingEnabled *bool
}

// NewOllamaProvider creates an OllamaProvider.
// apiBase is the Ollama root URL (e.g. "http://localhost:11434").
// The /v1 suffix is stripped automatically if present.
// apiKey is optional; Ollama does not require authentication by default.
// numCtx overrides the model's default context window when non-nil.
// httpClient may be nil to use a default client.
func NewOllamaProvider(name, apiBase, defaultModel string, numCtx *int, httpClient *http.Client) *OllamaProvider {
	// Strip trailing /v1 so the Ollama client reaches the root API.
	base := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(apiBase, "/"), "/v1"), "/")
	if base == "" {
		base = "http://localhost:11434"
	}

	parsedURL, err := url.Parse(base)
	if err != nil {
		slog.Warn("ollama: invalid api_base, falling back to localhost", "api_base", base, "error", err)
		parsedURL, _ = url.Parse("http://localhost:11434")
	}

	if httpClient == nil {
		httpClient = NewDefaultHTTPClient()
	}

	return &OllamaProvider{
		name:         name,
		apiBase:      base,
		defaultModel: defaultModel,
		numCtx:       numCtx,
		client:       ollamaapi.NewClient(parsedURL, httpClient),
		retryConfig:  DefaultRetryConfig(),
		ctxCache:     make(map[string]int),
	}
}

// resolveNumCtx returns the context window to request for a given model.
//
// An explicitly configured num_ctx always wins — it is the operator's lever for
// capping the KV cache when a model's full window would not fit in VRAM.
// Otherwise the model's own context length is read from /api/show and cached,
// bounded by OllamaDefaultNumCtx so an enormous advertised window (Qwen3.5
// reports 262144) cannot balloon the KV cache allocation.
//
// Never returns less than the caller would need: on any lookup failure it falls
// back to OllamaDefaultNumCtx rather than to Ollama's 4096 default, which
// silently rejects any prompt larger than a few thousand tokens.
func (p *OllamaProvider) resolveNumCtx(ctx context.Context, model string) int {
	if p.numCtx != nil {
		return *p.numCtx
	}

	p.ctxMu.RLock()
	cached, ok := p.ctxCache[model]
	p.ctxMu.RUnlock()
	if ok {
		return cached
	}

	numCtx := min(FetchOllamaModelContext(ctx, p.apiBase, model, p.apiKey), OllamaDefaultNumCtx)

	p.ctxMu.Lock()
	p.ctxCache[model] = numCtx
	p.ctxMu.Unlock()

	return numCtx
}

// WithThinkingEnabled sets the provider-level override for whether native
// Ollama chat requests should ask the model to emit visible reasoning
// ("think") tokens. nil (not calling this) preserves the existing default
// of disabling thinking.
func (p *OllamaProvider) WithThinkingEnabled(enabled *bool) *OllamaProvider {
	p.thinkingEnabled = enabled
	return p
}

// ThinkingEnabled returns the configured provider-level thinking override, or nil if not set.
func (p *OllamaProvider) ThinkingEnabled() *bool {
	return p.thinkingEnabled
}

// Name returns the provider identifier.
func (p *OllamaProvider) Name() string { return p.name }

// DefaultModel returns the default model.
func (p *OllamaProvider) DefaultModel() string { return p.defaultModel }

// APIBase returns the Ollama root URL with /v1 suffix, matching the format
// stored in the database and expected by tests that introspect the registered provider.
func (p *OllamaProvider) APIBase() string { return p.apiBase + "/v1" }

// Capabilities declares what Ollama supports.
func (p *OllamaProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         false,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: OllamaDefaultNumCtx,
		TokenizerID:      "",
	}
}

// Chat sends a non-streaming chat request to Ollama and returns the full response.
func (p *OllamaProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	var result *ChatResponse
	var chatErr error

	fn := func() (*ChatResponse, error) {
		var finalResp *ChatResponse
		streamFn := func(resp ollamaapi.ChatResponse) error {
			if resp.Done {
				finalResp = p.buildResponse(resp)
			}
			return nil
		}
		if err := p.client.Chat(ctx, p.buildRequest(ctx, req, false), streamFn); err != nil {
			return nil, fmt.Errorf("%s: chat: %w", p.name, err)
		}
		if finalResp == nil {
			finalResp = &ChatResponse{FinishReason: "stop"}
		}
		return finalResp, nil
	}

	result, chatErr = RetryDo(ctx, p.retryConfig, fn)
	return result, chatErr
}

// ChatStream sends a streaming chat request to Ollama, calling onChunk for each
// content delta, and returns the accumulated final response.
func (p *OllamaProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	result := &ChatResponse{FinishReason: "stop"}

	fn := func() (*ChatResponse, error) {
		acc := &ChatResponse{FinishReason: "stop"}

		streamFn := func(resp ollamaapi.ChatResponse) error {
			delta := resp.Message.Content
			if delta != "" {
				acc.Content += delta
				onChunk(StreamChunk{Content: delta})
			}

			thinking := resp.Message.Thinking
			if thinking != "" {
				acc.Thinking += thinking
				onChunk(StreamChunk{Thinking: thinking})
			}

			if resp.Done {
				// Tool calls come in the final response message.
				for _, tc := range resp.Message.ToolCalls {
					call := ToolCall{
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments.ToMap(),
					}
					acc.ToolCalls = append(acc.ToolCalls, call)
				}
				acc.FinishReason = mapDoneReason(resp.DoneReason)
				if len(acc.ToolCalls) > 0 && acc.FinishReason != "length" {
					acc.FinishReason = "tool_calls"
				}
				acc.Usage = &Usage{
					PromptTokens:     resp.PromptEvalCount,
					CompletionTokens: resp.EvalCount,
					TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
					RequestCount:     1,
				}
			}
			return nil
		}

		if err := p.client.Chat(ctx, p.buildRequest(ctx, req, true), streamFn); err != nil {
			return nil, fmt.Errorf("%s: chat stream: %w", p.name, err)
		}
		return acc, nil
	}

	var chatErr error
	result, chatErr = RetryDo(ctx, p.retryConfig, fn)
	return result, chatErr
}

// buildRequest converts a generic ChatRequest into an Ollama-native api.ChatRequest.
func (p *OllamaProvider) buildRequest(ctx context.Context, req ChatRequest, stream bool) *ollamaapi.ChatRequest {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	msgs := make([]ollamaapi.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := ollamaapi.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		// Map outgoing tool calls on assistant messages.
		for _, tc := range m.ToolCalls {
			args := ollamaapi.NewToolCallFunctionArguments()
			for k, v := range tc.Arguments {
				args.Set(k, v)
			}
			msg.ToolCalls = append(msg.ToolCalls, ollamaapi.ToolCall{
				ID: tc.ID,
				Function: ollamaapi.ToolCallFunction{
					Name:      tc.Name,
					Arguments: args,
				},
			})
		}
		msgs = append(msgs, msg)
	}

	ollamaReq := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   &stream,
	}

	// Thinking visibility: default off (models like qwq/deepseek-r1 have
	// thinking on by default and goclaw suppresses it to avoid bloated
	// chain-of-thought responses), unless the provider config explicitly
	// enables it via settings.thinking_enabled=true.
	thinkingEnabled := false
	if p.thinkingEnabled != nil {
		thinkingEnabled = *p.thinkingEnabled
	}
	ollamaReq.Think = &ollamaapi.ThinkValue{Value: thinkingEnabled}

	// Inject tools.
	for _, td := range req.Tools {
		if td.Type != "function" || td.Function == nil {
			continue
		}
		props := ollamaapi.NewToolPropertiesMap()
		if params, ok := td.Function.Parameters["properties"].(map[string]any); ok {
			for propName, propVal := range params {
				if propMap, ok2 := propVal.(map[string]any); ok2 {
					prop := ollamaapi.ToolProperty{}
					if desc, ok3 := propMap["description"].(string); ok3 {
						prop.Description = desc
					}
					if typ, ok3 := propMap["type"].(string); ok3 {
						prop.Type = ollamaapi.PropertyType{typ}
					}
					props.Set(propName, prop)
				}
			}
		}
		required, _ := td.Function.Parameters["required"].([]string)
		tool := ollamaapi.Tool{
			Type: "function",
			Function: ollamaapi.ToolFunction{
				Name:        td.Function.Name,
				Description: td.Function.Description,
				Parameters: ollamaapi.ToolFunctionParameters{
					Type:       "object",
					Required:   required,
					Properties: props,
				},
			},
		}
		ollamaReq.Tools = append(ollamaReq.Tools, tool)
	}

	// Build options map: num_ctx + caller overrides.
	// Always set num_ctx so Ollama uses a large context window even when the caller
	// did not configure a specific value. Without this, Ollama defaults to 4096 and
	// rejects conversations that exceed that limit. Note this only works because the
	// request goes to the native /api/chat endpoint — the OpenAI-compat shim at
	// /v1/chat/completions drops options.num_ctx on the floor.
	opts := make(map[string]any)
	opts["num_ctx"] = p.resolveNumCtx(ctx, model)
	if temp, ok := req.Options[OptTemperature]; ok {
		opts["temperature"] = temp
	}
	if maxTokens, ok := req.Options[OptMaxTokens]; ok {
		opts["num_predict"] = maxTokens
	}
	ollamaReq.Options = opts

	slog.Debug("ollama.request", "provider", p.name, "model", model, "num_ctx", opts["num_ctx"])
	return ollamaReq
}

// buildResponse converts a final Ollama ChatResponse into the generic ChatResponse.
func (p *OllamaProvider) buildResponse(resp ollamaapi.ChatResponse) *ChatResponse {
	result := &ChatResponse{
		Content:      resp.Message.Content,
		Thinking:     resp.Message.Thinking,
		FinishReason: mapDoneReason(resp.DoneReason),
	}

	for _, tc := range resp.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments.ToMap(),
		})
	}

	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}

	result.Usage = &Usage{
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
		TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		RequestCount:     1,
	}

	return result
}

// mapDoneReason converts an Ollama done_reason string to the standard finish_reason.
func mapDoneReason(reason string) string {
	switch reason {
	case "stop":
		return "stop"
	case "length":
		return "length"
	case "tool_calls", "function_call":
		return "tool_calls"
	default:
		return "stop"
	}
}
