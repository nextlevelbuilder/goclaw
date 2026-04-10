package providers

// PromptContribution holds provider-specific system prompt customizations.
// Providers implement PromptContributor to inject/override prompt sections.
type PromptContribution struct {
	StablePrefix     string            // injected before cache boundary (e.g. reasoning format)
	DynamicSuffix    string            // injected after cache boundary
	SectionOverrides map[string]string // override by section ID (replaces default content)
}

// Section ID constants for overridable sections.
// Safety is NOT overridable — blocked for security.
const (
	SectionIDExecutionBias = "execution_bias"
	SectionIDToolCallStyle = "tool_call_style"
)

// PromptContributor is optionally implemented by providers needing prompt customization.
// Nil-safe: type assertion returns nil for providers that don't implement this.
type PromptContributor interface {
	PromptContribution() *PromptContribution
}
