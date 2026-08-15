package providers

// CleanToolSchemas normalizes tool schemas for a specific provider.
// This is the batch entry point — called from OpenAI/DashScope providers.
// Native tool types (anything other than "function") are passed through untouched.
func CleanToolSchemas(providerName string, tools []ToolDefinition) []ToolDefinition {
	if len(tools) == 0 {
		return tools
	}
	profile := profileForProvider(providerName)
	out := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		switch t.Type {
		case "function":
			if t.Function == nil {
				// Malformed function tool — skip rather than panic.
				continue
			}
			fn := cleanFunctionSchema(profile, *t.Function)
			out = append(out, ToolDefinition{
				Type:     "function",
				Function: &fn,
			})
		default:
			// Native provider tool (e.g. "image_generation") — pass through as-is.
			out = append(out, t)
		}
	}
	return out
}

// cleanFunctionSchema normalizes a single function tool schema against a provider profile.
// Returns a new ToolFunctionSchema with cleaned parameters and strict mode applied.
func cleanFunctionSchema(profile SchemaProfile, fn ToolFunctionSchema) ToolFunctionSchema {
	// Exempt multi-action tools from strict mode — their many optional params become
	// required under strict, forcing models to send empty values (~200-300 wasted tokens/call).
	useStrict := profile.StrictToolMode && !IsMultiActionSchema(fn.Parameters)

	var strictPtr *bool
	if useStrict {
		tr := true
		strictPtr = &tr
	}

	toolProfile := profile
	toolProfile.StrictToolMode = useStrict

	return ToolFunctionSchema{
		Name:        fn.Name,
		Description: fn.Description,
		Parameters:  normalizeWithProfile(toolProfile, fn.Parameters),
		Strict:      strictPtr,
	}
}

// CleanSchemaForProvider normalizes a single tool's parameters.
// Called from the Anthropic provider.
func CleanSchemaForProvider(providerName string, params map[string]any) map[string]any {
	return NormalizeSchema(providerName, params)
}
