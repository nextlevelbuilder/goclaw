package providers

import "strings"

// CapabilityLevel rates how well a model family supports a given capability.
// Values are static negotiation hints for fallback selection, not a runtime
// contract: the provider is the source of truth when a call actually runs.
type CapabilityLevel string

const (
	CapabilityStrong CapabilityLevel = "strong"
	CapabilityMedium CapabilityLevel = "medium"
	CapabilityNone   CapabilityLevel = "none"
)

// ModelCapabilityProfile describes what a well-known model family supports.
// MaxContext is the approximate context window in tokens; 0 means unknown.
type ModelCapabilityProfile struct {
	ToolCalling      CapabilityLevel
	StructuredOutput CapabilityLevel
	Reasoning        CapabilityLevel
	Streaming        CapabilityLevel
	Vision           CapabilityLevel
	MaxContext       int
}

// SupportsToolCalls reports whether the profile can drive tool/function calls.
func (p ModelCapabilityProfile) SupportsToolCalls() bool {
	return p.ToolCalling == CapabilityStrong || p.ToolCalling == CapabilityMedium
}

// SupportsStreaming reports whether the profile can stream responses.
func (p ModelCapabilityProfile) SupportsStreaming() bool {
	return p.Streaming == CapabilityStrong || p.Streaming == CapabilityMedium
}

// defaultCapabilityProfile is used for any model without a known entry. It is
// deliberately permissive: unknown models must never be blocked from tool
// calls or streaming, which would silently degrade every configured agent.
var defaultCapabilityProfile = ModelCapabilityProfile{
	ToolCalling:      CapabilityStrong,
	StructuredOutput: CapabilityMedium,
	Reasoning:        CapabilityMedium,
	Streaming:        CapabilityStrong,
	Vision:           CapabilityNone,
	MaxContext:       0,
}

// qwenVLProfile covers Qwen vision-language variants (qwen-vl-max,
// qwen2-vl-72b, ...). Plain qwen chat models are vision-less; the cheaper
// "vl" substring check runs before the shared table below.
var qwenVLProfile = ModelCapabilityProfile{
	ToolCalling:      CapabilityStrong,
	StructuredOutput: CapabilityMedium,
	Reasoning:        CapabilityMedium,
	Streaming:        CapabilityStrong,
	Vision:           CapabilityStrong,
	MaxContext:       131072,
}

// capabilityProfiles maps model-id prefixes to known profiles. The list is
// intentionally small (15 well-known families): these are fallback-selection
// hints, not a model registry. More specific prefixes must come first; the
// first match wins. Models not covered fall back to defaultCapabilityProfile.
var capabilityProfiles = []struct {
	prefix  string
	profile ModelCapabilityProfile
}{
	{
		prefix:  "gpt-4o",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityStrong, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityStrong, MaxContext: 128000},
	},
	{
		prefix:  "claude-3-5-sonnet",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityStrong, Reasoning: CapabilityStrong, Streaming: CapabilityStrong, Vision: CapabilityMedium, MaxContext: 200000},
	},
	{
		prefix:  "claude-3-5-haiku",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityMedium, MaxContext: 200000},
	},
	{
		prefix:  "claude-sonnet-4",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityStrong, Reasoning: CapabilityStrong, Streaming: CapabilityStrong, Vision: CapabilityMedium, MaxContext: 200000},
	},
	{
		prefix:  "claude-opus-4",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityStrong, Reasoning: CapabilityStrong, Streaming: CapabilityStrong, Vision: CapabilityMedium, MaxContext: 200000},
	},
	{
		prefix:  "gemini-2",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityStrong, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityStrong, MaxContext: 1048576},
	},
	{
		prefix:  "gemini-1.5-pro",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityStrong, MaxContext: 2097152},
	},
	{
		prefix:  "gemini-1.5-flash",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityStrong, MaxContext: 1048576},
	},
	{
		prefix: "deepseek-reasoner",
		// The reasoner (R1) does not expose function calling through the
		// chat-completions API, so tool calls must not be routed to it.
		profile: ModelCapabilityProfile{ToolCalling: CapabilityNone, StructuredOutput: CapabilityMedium, Reasoning: CapabilityStrong, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 128000},
	},
	{
		prefix:  "deepseek-chat",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 128000},
	},
	{
		// Plain Qwen chat models (qwen-max, qwen-turbo, qwen2.5-*). Vision
		// variants carry "vl" in the id and are handled before this table.
		prefix:  "qwen",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 131072},
	},
	{
		// llama-3.x (3.1/3.2/3.3) support tool calling via the message API.
		// Vision variants (llama-3.2-*-vision) are not distinguished here.
		prefix:  "llama-3",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 131072},
	},
	{
		// mistral-* (La Plateforme medium/large families). Newer vision-
		// capable large variants are not distinguished here.
		prefix:  "mistral",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 131072},
	},
	{
		// phi-3 family: no native function-calling API; routing tool calls
		// to it would fail at runtime, so it is excluded from tool fallback.
		prefix:  "phi-3",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityNone, StructuredOutput: CapabilityMedium, Reasoning: CapabilityMedium, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 128000},
	},
	{
		prefix:  "codex",
		profile: ModelCapabilityProfile{ToolCalling: CapabilityStrong, StructuredOutput: CapabilityMedium, Reasoning: CapabilityStrong, Streaming: CapabilityStrong, Vision: CapabilityNone, MaxContext: 100000},
	},
}

// ProfileFor returns the capability profile for a model id, using prefix
// matching against the known families and defaultCapabilityProfile otherwise.
func ProfileFor(model string) ModelCapabilityProfile {
	// Qwen vision variants carry "vl" in the model id (qwen-vl-max,
	// qwen2-vl-72b, ...). Cheap prefix match before the shared table.
	if strings.HasPrefix(model, "qwen") && strings.Contains(model, "vl") {
		return qwenVLProfile
	}
	for _, entry := range capabilityProfiles {
		if strings.HasPrefix(model, entry.prefix) {
			return entry.profile
		}
	}
	return defaultCapabilityProfile
}