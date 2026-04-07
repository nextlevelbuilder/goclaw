package providers

import "strings"

// OpenAIForwardCompat resolves unknown OpenAI models by cloning from known templates.
type OpenAIForwardCompat struct{}

// openAIForwardCompat maps unknown model prefixes to template + optional spec patch.
var openAIForwardCompatMap = map[string]struct {
	Template string
	Patch    *ModelSpec
}{
	"gpt-5.5": {
		Template: "gpt-5.4",
		Patch:    &ModelSpec{ContextWindow: 1_000_000, MaxTokens: 200_000},
	},
	"gpt-5.6": {
		Template: "gpt-5.4",
		Patch:    &ModelSpec{ContextWindow: 2_000_000, MaxTokens: 200_000},
	},
	"o5-mini": {
		Template: "o4-mini",
		Patch:    &ModelSpec{ContextWindow: 300_000},
	},
}

// ResolveForwardCompat handles models like "gpt-5.5" by cloning from "gpt-5.4".
func (r *OpenAIForwardCompat) ResolveForwardCompat(modelID string, registry ModelRegistry) *ModelSpec {
	// Direct map lookup (exact match)
	if entry, ok := openAIForwardCompatMap[modelID]; ok {
		return CloneFromTemplate(registry, "openai", modelID, []string{entry.Template}, entry.Patch)
	}

	// Try prefix matching for versioned models like "gpt-5.5-turbo"
	for prefix, entry := range openAIForwardCompatMap {
		if strings.HasPrefix(modelID, prefix) {
			return CloneFromTemplate(registry, "openai", modelID, []string{entry.Template}, entry.Patch)
		}
	}

	return nil
}
