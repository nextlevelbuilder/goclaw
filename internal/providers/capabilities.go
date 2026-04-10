package providers

import "net/http"

// ProviderCapabilities declares what a provider supports.
// Queried by pipeline to choose code paths (streaming vs non-streaming, etc.)
type ProviderCapabilities struct {
	Streaming        bool   // supports ChatStream()
	ToolCalling      bool   // supports tools in request
	StreamWithTools  bool   // can stream while tool calls are in-flight
	Thinking         bool   // supports extended thinking / reasoning
	Vision           bool   // supports image inputs
	CacheControl     bool   // supports cache_control blocks (Anthropic)
	MaxContextWindow int    // default context window for default model
	TokenizerID      string // for tokencount package mapping
}

// CapabilitiesAware is optionally implemented by Provider.
// Pipeline checks this to choose code path.
type CapabilitiesAware interface {
	Capabilities() ProviderCapabilities
}

// ProviderAdapter transforms between internal wire format and provider-specific format.
// Used internally by each Provider implementation to separate serialization from transport.
// The existing Provider interface (Chat/ChatStream) is unchanged — ProviderAdapter is
// composed inside each provider, not a replacement.
type ProviderAdapter interface {
	// ToRequest converts internal ChatRequest to provider-specific wire bytes.
	ToRequest(req ChatRequest) ([]byte, http.Header, error)

	// FromResponse converts provider-specific response bytes to internal ChatResponse.
	FromResponse(data []byte) (*ChatResponse, error)

	// FromStreamChunk converts a single SSE chunk to internal StreamChunk.
	// Returns nil if chunk should be skipped (keep-alive, metadata).
	FromStreamChunk(data []byte) (*StreamChunk, error)

	// Capabilities returns static capability declaration.
	Capabilities() ProviderCapabilities

	// Name returns provider identifier.
	Name() string
}

// AdapterFactory creates a ProviderAdapter from config.
type AdapterFactory func(cfg ProviderConfig) (ProviderAdapter, error)

// ProviderConfig is passed to AdapterFactory during registration.
// Wraps the fields needed to construct a provider-specific adapter.
type ProviderConfig struct {
	Name      string
	BaseURL   string
	APIKey    string
	Model     string
	ExtraOpts map[string]any
}
