package providers

import (
	"context"
	"encoding/json"
)

// Options keys used in ChatRequest.Options across providers.
const (
	OptMaxTokens      = "max_tokens"
	OptTemperature    = "temperature"
	OptThinkingLevel  = "thinking_level"
	OptReasoningEffort = "reasoning_effort"
	OptEnableThinking = "enable_thinking"
	OptThinkingBudget = "thinking_budget"
)

// Provider is the interface all LLM providers must implement.
type Provider interface {
	// Chat sends messages to the LLM and returns a response.
	// tools defines available tool schemas; model overrides the default.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream sends messages and streams response chunks via callback.
	// Returns the final complete response after streaming ends.
	ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error)

	// DefaultModel returns the provider's default model name.
	DefaultModel() string

	// Name returns the provider identifier (e.g. "anthropic", "openai").
	Name() string
}

// ThinkingCapable is optionally implemented by providers that support extended thinking.
// Used to gate thinking_level injection so it's not sent to providers that ignore it.
type ThinkingCapable interface {
	SupportsThinking() bool
}

// ChatRequest contains the input for a Chat/ChatStream call.
type ChatRequest struct {
	Messages []Message              `json:"messages"`
	Tools    []ToolDefinition       `json:"tools,omitempty"`
	Model    string                 `json:"model,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// ChatResponse is the result from an LLM call.
type ChatResponse struct {
	Content      string     `json:"content"`
	Thinking     string     `json:"thinking,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"` // "stop", "tool_calls", "length"
	Usage        *Usage     `json:"usage,omitempty"`

	// RawAssistantContent preserves the raw content blocks array from the provider response.
	// Used by Anthropic to pass thinking blocks back in tool use loops (required by API).
	RawAssistantContent json.RawMessage `json:"-"`
}

// StreamChunk is a piece of a streaming response.
type StreamChunk struct {
	Content   string `json:"content,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Done      bool   `json:"done,omitempty"`
}

// ImageContent represents a base64-encoded image for vision-capable models.
type ImageContent struct {
	MimeType string `json:"mime_type"` // e.g. "image/jpeg"
	Data     string `json:"data"`      // base64-encoded image bytes
}

// Message represents a conversation message.
type Message struct {
	Role       string         `json:"role"`                  // "system", "user", "assistant", "tool"
	Content    string         `json:"content"`
	Thinking   string         `json:"thinking,omitempty"`    // reasoning_content for thinking models (Kimi, DeepSeek, etc.)
	Images     []ImageContent `json:"images,omitempty"`      // vision: base64 images
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"` // for role="tool" responses

	// RawAssistantContent carries raw provider content blocks through tool loop iterations.
	// Anthropic requires thinking blocks to be passed back exactly as received.
	RawAssistantContent json.RawMessage `json:"-"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Metadata  map[string]string      `json:"metadata,omitempty"` // provider-specific (e.g. Gemini thought_signature)
}

// ToolDefinition describes a tool available to the LLM.
type ToolDefinition struct {
	Type     string             `json:"type"` // "function"
	Function ToolFunctionSchema `json:"function"`
}

// ToolFunctionSchema is the schema for a function tool.
type ToolFunctionSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_input_tokens,omitempty"`
	ThinkingTokens      int `json:"thinking_tokens,omitempty"`
}
