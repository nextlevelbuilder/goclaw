package providers

import (
	"context"
	"encoding/json"
	"time"
)

// Options keys used in ChatRequest.Options across providers.
const (
	OptMaxTokens       = "max_tokens"
	OptTemperature     = "temperature"
	OptThinkingLevel   = "thinking_level"
	OptReasoningEffort = "reasoning_effort"
	OptEnableThinking  = "enable_thinking"
	OptThinkingBudget  = "thinking_budget"
	// OptStripThinking (bool) tells stream handlers to drop reasoning tokens
	// from ChatResponse.Thinking and onChunk callbacks. Usage.ThinkingTokens
	// and RawAssistantContent are preserved (billing + tool passback safety).
	OptStripThinking = "strip_thinking"

	// Middleware-related options (Phase 2 will use these)
	OptServiceTier          = "service_tier"
	OptFastMode             = "fast_mode"
	OptPromptCacheKey       = "prompt_cache_key"
	OptPromptCacheRetention = "prompt_cache_retention"
)

// TokenSource provides an OAuth access token (with auto-refresh).
type TokenSource interface {
	Token() (string, error)
}

type RouteEligibilityClass string

const (
	RouteEligibilityHealthy RouteEligibilityClass = "healthy"
	RouteEligibilityUnknown RouteEligibilityClass = "unknown"
	RouteEligibilityBlocked RouteEligibilityClass = "blocked"
)

type RouteEligibility struct {
	Class  RouteEligibilityClass
	Reason string
}

type RouteEligibilityAware interface {
	RouteEligibility(ctx context.Context) RouteEligibility
}

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
	Messages []Message        `json:"messages"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
	Model    string           `json:"model,omitempty"`
	Options  map[string]any   `json:"options,omitempty"`
}

// ChatResponse is the result from an LLM call.
type ChatResponse struct {
	Content      string     `json:"content"`
	Thinking     string     `json:"thinking,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"` // "stop", "tool_calls", "length"
	Usage        *Usage     `json:"usage,omitempty"`

	// Phase is Codex-specific (gpt-5.3-codex): "commentary" or "final_answer".
	// Agent loop must persist this on assistant messages for Codex performance.
	Phase string `json:"phase,omitempty"`

	// RawAssistantContent preserves the raw content blocks array from the provider response.
	// Used by Anthropic to pass thinking blocks back in tool use loops (required by API).
	RawAssistantContent json.RawMessage `json:"-"`

	// ThinkingSignature is the accumulated signature from streaming thinking blocks.
	// Required by Anthropic API for tool use passback when thinking is enabled.
	ThinkingSignature string `json:"-"`

	// Images holds generated images returned by image_generation_call tools (Codex).
	// Not persisted to DB; populated at runtime from provider response.
	Images []ImageContent `json:"-"`
}

// StreamChunk is a piece of a streaming response.
type StreamChunk struct {
	Content  string         `json:"content,omitempty"`
	Thinking string         `json:"thinking,omitempty"`
	Done     bool           `json:"done,omitempty"`
	Images   []ImageContent `json:"images,omitempty"` // image generation frames (Codex)
}

// ImageContent represents a base64-encoded image for vision-capable models.
type ImageContent struct {
	MimeType string `json:"mime_type"`         // e.g. "image/jpeg"
	Data     string `json:"data"`              // base64-encoded image bytes
	Partial  bool   `json:"partial,omitempty"` // true for intermediate frames (Codex image_generation_call)
}

// MediaRef is a lightweight reference to a persistently stored media file.
// Stored in session JSONB (~60 bytes each) instead of megabytes for base64.
// On reload, MediaRefs are resolved to file paths and loaded into Images (for images).
type MediaRef struct {
	ID       string `json:"id"`               // unique media ID (uuid)
	MimeType string `json:"mime_type"`        // e.g. "image/jpeg", "application/pdf"
	Kind     string `json:"kind"`             // "image", "video", "audio", "document"
	Path     string `json:"path,omitempty"`   // absolute workspace path (persisted for /v1/files/ serving)
	Prompt   string `json:"prompt,omitempty"` // prompt that generated this asset, if known
}

// Message represents a conversation message.
type Message struct {
	Role       string         `json:"role"` // "system", "user", "assistant", "tool"
	Content    string         `json:"content"`
	Thinking   string         `json:"thinking,omitempty"`   // reasoning_content for thinking models (Kimi, DeepSeek, etc.)
	Images     []ImageContent `json:"-"`                    // vision: base64 images (runtime only, never persisted to DB)
	MediaRefs  []MediaRef     `json:"media_refs,omitempty"` // persistent media file references
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"` // for role="tool" responses
	IsError    bool           `json:"is_error,omitempty"`     // for role="tool" responses

	// Phase is a Codex-specific field (gpt-5.3-codex) indicating message purpose.
	// Values: "commentary" (intermediate), "final_answer" (closeout), or "" (unset).
	// Must be persisted and passed back in subsequent requests for Codex performance.
	// Other providers ignore this field.
	Phase string `json:"phase,omitempty"`

	// RawAssistantContent carries raw provider content blocks through tool loop iterations.
	// Anthropic requires thinking blocks to be passed back exactly as received.
	RawAssistantContent json.RawMessage `json:"-"`

	// CreatedAt records when this message was added to the session.
	// Pointer type so that older messages (stored before this field existed) deserialize as nil,
	// allowing the frontend to fall back to synthetic timestamps.
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Arguments  map[string]any    `json:"arguments"`
	Metadata   map[string]string `json:"metadata,omitempty"`    // provider-specific (e.g. Gemini thought_signature)
	ParseError string            `json:"parse_error,omitempty"` // set when arguments JSON was malformed/truncated
}

// ToolDefinition describes a tool available to the LLM.
// Type is "function" for standard function tools, or a native provider tool type
// (e.g. "image_generation") for first-class provider-native tools.
// Function is nil when Type is not "function".
type ToolDefinition struct {
	Type     string              `json:"type"`               // "function" | "image_generation" | ...
	Function *ToolFunctionSchema `json:"function,omitempty"` // nil when Type != "function"
}

// ToolFunctionSchema is the schema for a function tool.
type ToolFunctionSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      *bool          `json:"strict,omitempty"` // OpenAI strict mode — constrained decoding
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
