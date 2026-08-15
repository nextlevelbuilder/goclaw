package providers

import "strings"

// SchemaProfile controls which normalizations apply to a provider's tool schemas.
// Add new fields here to introduce additional transforms; implement them in
// schema_normalize.go and wire them in NormalizeSchema.
type SchemaProfile struct {
	ResolveRefs       bool     // inline $ref from $defs/definitions
	FlattenUnions     bool     // merge anyOf/oneOf → single object
	InjectObjectType  bool     // add type:"object" when missing but has properties/required
	ConvertConst      bool     // const → enum (Gemini requires enum, not const)
	StripNullType     bool     // anyOf:[T, null] → T
	RemoveTypeOnUnion bool     // strip "type" when anyOf/oneOf present (Gemini conflict)
	StripKeys         []string // keys to recursively remove
	StrictToolMode    bool     // OpenAI strict mode: optional→nullable, all props required, additionalProperties:false
}

// Provider-specific strip key lists.
var (
	geminiStripKeys = []string{
		"$ref", "$defs", "definitions", "additionalProperties",
		"patternProperties", "$schema", "$id",
		"examples", "default",
		"minLength", "maxLength", "minimum", "maximum", "multipleOf",
		"pattern", "format",
		"minItems", "maxItems", "uniqueItems",
		"minProperties", "maxProperties",
	}
	xaiStripKeys = []string{
		"minLength", "maxLength",
		"minItems", "maxItems",
		"minContains", "maxContains",
	}
	refOnlyStripKeys = []string{"$ref", "$defs", "definitions"}
)

// profileForProvider returns the normalization profile for a provider.
// Unknown providers get a safe default (resolve + flatten + inject type).
func profileForProvider(name string) SchemaProfile {
	switch {
	case name == "anthropic":
		return SchemaProfile{
			ResolveRefs: true,
			StripKeys:   refOnlyStripKeys,
		}
	case isGeminiName(name):
		return SchemaProfile{
			ResolveRefs:       true,
			FlattenUnions:     true,
			ConvertConst:      true,
			StripNullType:     true,
			RemoveTypeOnUnion: true,
			StripKeys:         geminiStripKeys,
		}
	case name == "xai" || strings.HasPrefix(name, "xai-"):
		return SchemaProfile{
			ResolveRefs:      true,
			FlattenUnions:    true,
			InjectObjectType: true,
			StripKeys:        xaiStripKeys,
		}
	case isOpenAIStrict(name):
		return SchemaProfile{
			ResolveRefs:      true,
			FlattenUnions:    true,
			InjectObjectType: true,
			StrictToolMode:   true,
			StripKeys:        refOnlyStripKeys,
		}
	default: // openrouter, deepseek, groq, dashscope, bailian, minimax, etc.
		return SchemaProfile{
			ResolveRefs:      true,
			FlattenUnions:    true,
			InjectObjectType: true,
			StripKeys:        refOnlyStripKeys,
		}
	}
}

// isOpenAIStrict returns true for providers known to support strict tool mode.
// Matches first-party OpenAI (including chatgpt_oauth) and Codex.
// Explicitly excludes openai_compat (proxy for OpenRouter, DeepSeek, Groq, etc.).
func isOpenAIStrict(name string) bool {
	lower := strings.ToLower(name)
	// Exclude compat/proxy providers first — they route to non-OpenAI models.
	if strings.Contains(lower, "compat") {
		return false
	}
	switch {
	case lower == "openai" || lower == "codex":
		return true
	case strings.Contains(lower, "chatgpt"):
		return true // chatgpt_oauth, chatgpt_plus, etc.
	}
	return false
}

// isGeminiName matches config names ("gemini", "gemini-flash") and
// DB provider types ("gemini_native"). Uses Contains for robustness
// with user-defined names (e.g. "my-gemini-proxy").
func isGeminiName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "gemini")
}
