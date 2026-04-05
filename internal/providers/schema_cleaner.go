package providers

// CleanToolSchemas normalizes tool schemas for a specific provider.
// This is the batch entry point — called from OpenAI/DashScope providers.
func CleanToolSchemas(providerName string, tools []ToolDefinition) []ToolDefinition {
	if len(tools) == 0 {
		return tools
	}
	profile := profileForProvider(providerName)
	cleaned := make([]ToolDefinition, len(tools))
	for i, t := range tools {
		// Exempt multi-action tools from strict mode — their many optional
		// params become required under strict, forcing models to send empty values
		// for every call (wasting ~200-300 output tokens per tool call).
		useStrict := profile.StrictToolMode && !IsMultiActionSchema(t.Function.Parameters)

		var strictPtr *bool
		if useStrict {
			tr := true
			strictPtr = &tr
		}

		toolProfile := profile
		toolProfile.StrictToolMode = useStrict

		cleaned[i] = ToolDefinition{
			Type: t.Type,
			Function: ToolFunctionSchema{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  normalizeWithProfile(toolProfile, t.Function.Parameters),
				Strict:      strictPtr,
			},
		}
	}
	return cleaned
}

// CleanSchemaForProvider normalizes a single tool's parameters.
// Called from the Anthropic provider.
func CleanSchemaForProvider(providerName string, params map[string]any) map[string]any {
	return NormalizeSchema(providerName, params)
}
