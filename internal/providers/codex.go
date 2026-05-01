package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type CodexRoutingDefaults struct {
	Strategy           string
	ExtraProviderNames []string
}

// CodexProvider implements Provider for the OpenAI Responses API,
// used with ChatGPT subscription via OAuth (Codex flow).
// Wire format: POST /codex/responses on chatgpt.com backend.
type CodexProvider struct {
	name            string
	apiBase         string // e.g. "https://api.openai.com/v1" or "https://chatgpt.com/backend-api"
	defaultModel    string
	client          *http.Client
	retryConfig     RetryConfig
	middlewares     RequestMiddleware // composed middleware chain (nil = no-op)
	tokenSource     TokenSource
	routingDefaults *CodexRoutingDefaults
}

// NewCodexProvider creates a provider for the OpenAI Responses API with OAuth token.
func NewCodexProvider(name string, tokenSource TokenSource, apiBase, defaultModel string) *CodexProvider {
	if apiBase == "" {
		apiBase = "https://chatgpt.com/backend-api"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	if defaultModel == "" {
		defaultModel = "gpt-5.4"
	}

	return &CodexProvider{
		name:         name,
		apiBase:      apiBase,
		defaultModel: defaultModel,
		client:       NewDefaultHTTPClient(),
		retryConfig:  DefaultRetryConfig(),
		tokenSource:  tokenSource,
	}
}

// WithMiddlewares sets the composed request middleware chain.
func (p *CodexProvider) WithMiddlewares(mws ...RequestMiddleware) *CodexProvider {
	p.middlewares = ComposeMiddlewares(mws...)
	return p
}

// WithRetryConfig overrides the default per-provider retry config. Useful for
// tests and for callers that manage retry semantics at a higher layer (e.g.
// the pool router fails over on single-attempt member errors).
func (p *CodexProvider) WithRetryConfig(rc RetryConfig) *CodexProvider {
	p.retryConfig = rc
	return p
}

func (p *CodexProvider) Name() string           { return p.name }
func (p *CodexProvider) DefaultModel() string   { return p.defaultModel }
func (p *CodexProvider) SupportsThinking() bool { return true }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *CodexProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           true,
		CacheControl:     false,
		ImageGeneration:  true, // Codex (OpenAI Responses API) supports native image_generation tool
		MaxContextWindow: 1_000_000,
		TokenizerID:      "o200k_base",
	}
}

func (p *CodexProvider) WithRoutingDefaults(strategy string, extraProviderNames []string) *CodexProvider {
	p.routingDefaults = &CodexRoutingDefaults{
		Strategy:           strategy,
		ExtraProviderNames: append([]string(nil), extraProviderNames...),
	}
	return p
}
func (p *CodexProvider) RoutingDefaults() *CodexRoutingDefaults {
	if p.routingDefaults == nil {
		return nil
	}
	return &CodexRoutingDefaults{
		Strategy:           p.routingDefaults.Strategy,
		ExtraProviderNames: append([]string(nil), p.routingDefaults.ExtraProviderNames...),
	}
}
func (p *CodexProvider) RouteEligibility(ctx context.Context) RouteEligibility {
	if aware, ok := p.tokenSource.(RouteEligibilityAware); ok {
		return aware.RouteEligibility(ctx)
	}
	return RouteEligibility{Class: RouteEligibilityHealthy}
}

func (p *CodexProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Codex Responses API requires stream=true; delegate to ChatStream with no chunk handler.
	return p.ChatStream(ctx, req, nil)
}

// middlewareConfig builds a MiddlewareConfig for the current request.
func (p *CodexProvider) middlewareConfig(req ChatRequest) MiddlewareConfig {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	return MiddlewareConfig{
		Provider: p.name,
		Model:    model,
		Caps:     p.Capabilities(),
		AuthType: "oauth",
		APIBase:  p.apiBase,
		Options:  req.Options,
	}
}

func (p *CodexProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	// stripThinking: drop reasoning summaries from ChatResponse.Thinking and
	// onChunk callbacks. Usage.ThinkingTokens is still populated from the
	// final response.usage payload (Phase 1 billing accuracy).
	stripThinking, _ := req.Options[OptStripThinking].(bool)
	body := p.buildRequestBody(req, true)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(req))

	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	// Wrap respBody so ctx cancellation closes the socket, unblocking bufio.Scanner.
	cb := NewCtxBody(ctx, respBody)
	defer cb.Close()

	result := &ChatResponse{FinishReason: "stop"}
	toolCalls := make(map[string]*codexToolCallAcc) // keyed by item_id
	streamState := newCodexMessageStreamState()
	imageState := newCodexImageState()

	sse := NewSSEScanner(cb)
	for sse.Next() {
		data := sse.Data()

		var event codexSSEEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if err := p.processSSEEvent(&event, result, toolCalls, streamState, imageState, onChunk, stripThinking); err != nil {
			return nil, err
		}
	}

	if err := sse.Err(); err != nil {
		return nil, fmt.Errorf("%s: stream read error: %w", p.name, err)
	}

	// Assemble generated images from image accumulator into ChatResponse.
	imageState.appendToResponse(result)

	// Build tool calls from accumulators
	for _, acc := range toolCalls {
		if acc.name == "" {
			continue
		}
		args := make(map[string]any)
		var parseErr string
		if err := json.Unmarshal([]byte(acc.rawArgs), &args); err != nil && acc.rawArgs != "" {
			parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(acc.rawArgs), err)
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:         acc.callID,
			Name:       acc.name,
			Arguments:  args,
			ParseError: parseErr,
		})
	}

	// Only override finish_reason when response wasn't truncated.
	// Preserve "length" so agent loop can detect truncation and retry.
	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}

	if onChunk != nil {
		onChunk(StreamChunk{Done: true})
	}

	return result, nil
}

// processSSEEvent handles a single SSE event during streaming.
// stripThinking drops reasoning summaries from user-visible output while
// leaving billing counters (Usage.ThinkingTokens) untouched.
func (p *CodexProvider) processSSEEvent(event *codexSSEEvent, result *ChatResponse, toolCalls map[string]*codexToolCallAcc, streamState *codexMessageStreamState, imageState *codexImageState, onChunk func(StreamChunk), stripThinking bool) error {
	switch event.Type {
	case "response.image_generation_call.partial_image":
		// Intermediate frame from a streaming image generation call.
		// Deduplicate by SHA256 so identical frames are not re-emitted.
		if imageState.recordPartial(event.ItemID, event.OutputFormat, event.PartialImageB64) {
			if onChunk != nil {
				onChunk(StreamChunk{Images: []ImageContent{{
					MimeType: mimeFromFormat(event.OutputFormat),
					Data:     event.PartialImageB64,
					Partial:  true,
				}}})
			}
		}

	case "response.output_item.added":
		if event.Item != nil {
			streamState.registerMessageItem(event.ItemID, event.OutputIndex, event.Item)
		}

	case "response.output_text.delta":
		streamState.recordTextDelta(event.ItemID, event.OutputIndex, event.ContentIndex, event.Delta, result, onChunk)

	case "response.output_text.done":
		streamState.recordFinalText(event.ItemID, event.OutputIndex, event.ContentIndex, event.Text, result, onChunk)

	case "response.content_part.done":
		if event.Part != nil && event.Part.Type == "output_text" {
			streamState.recordFinalText(event.ItemID, event.OutputIndex, event.ContentIndex, event.Part.Text, result, onChunk)
		}

	case "response.function_call_arguments.delta":
		if event.ItemID != "" {
			acc := toolCalls[event.ItemID]
			if acc == nil {
				acc = &codexToolCallAcc{}
				toolCalls[event.ItemID] = acc
			}
			acc.rawArgs += event.Delta
		}

	case "response.output_item.done":
		if event.Item != nil {
			switch event.Item.Type {
			case "message":
				streamState.registerMessageItem(event.ItemID, event.OutputIndex, event.Item)
				streamState.flushMessage(codexEventItemKey(event.ItemID, event.Item), result, onChunk)
				streamState.updateResultPhase(result)
			case "function_call":
				acc := toolCalls[event.Item.ID]
				if acc == nil {
					acc = &codexToolCallAcc{}
				}
				acc.callID = event.Item.CallID
				acc.name = event.Item.Name
				if event.Item.Arguments != "" {
					acc.rawArgs = event.Item.Arguments
				}
				toolCalls[event.Item.ID] = acc
			case "reasoning":
				if !stripThinking {
					for _, s := range event.Item.Summary {
						if s.Text != "" {
							result.Thinking += s.Text
							if onChunk != nil {
								onChunk(StreamChunk{Thinking: s.Text})
							}
						}
					}
				}
			case "image_generation_call":
				// Final image for this item. Record and emit a non-partial chunk.
				itemID := event.Item.ID
				if itemID == "" {
					itemID = event.ItemID
				}
				imageState.recordFinal(itemID, event.Item.OutputFormat, event.Item.Result)
				if event.Item.Result != "" && onChunk != nil {
					onChunk(StreamChunk{Images: []ImageContent{{
						MimeType: mimeFromFormat(event.Item.OutputFormat),
						Data:     event.Item.Result,
						Partial:  false,
					}}})
				}
			}
		}

	case "response.completed", "response.incomplete":
		if event.Response != nil {
			if result.Content == "" {
				streamState.ingestCompletedResponse(event.Response)
				streamState.flushCompletedResponse(result, onChunk)
				streamState.updateResultPhase(result)
			}
			// Walk output[] for image_generation_call items not captured via stream events.
			// This covers non-streaming mode (single response.completed with all outputs)
			// and acts as a safety net for the streaming case.
			for i := range event.Response.Output {
				item := &event.Response.Output[i]
				if item.Type == "image_generation_call" && item.Result != "" {
					itemID := item.ID
					imageState.recordFinal(itemID, item.OutputFormat, item.Result)
				}
			}
			if event.Response.Usage != nil {
				u := event.Response.Usage
				result.Usage = &Usage{
					PromptTokens:     u.InputTokens,
					CompletionTokens: u.OutputTokens,
					TotalTokens:      u.TotalTokens,
				}
				if u.OutputTokensDetails != nil {
					result.Usage.ThinkingTokens = u.OutputTokensDetails.ReasoningTokens
				}
			}
			if event.Response.Status == "incomplete" {
				result.FinishReason = "length"
			}
		}

	case "response.failed":
		errMsg := "codex: response failed during generation"
		if event.Response != nil && event.Response.Error != nil {
			if event.Response.Error.Message != "" {
				errMsg = fmt.Sprintf("codex: response failed: %s", event.Response.Error.Message)
			} else if event.Response.Error.Code != "" {
				errMsg = fmt.Sprintf("codex: response failed: %s", event.Response.Error.Code)
			}
		}
		return errors.New(errMsg)
	}
	return nil
}
