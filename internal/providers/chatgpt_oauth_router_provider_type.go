package providers

// ProviderType implements typedProvider for ChatGPTOAuthRouter.
// Returns "chatgpt_oauth" for log/type-routing purposes.
// Image gen path in create_image.go short-circuits on _native_provider type-assert
// before reading _provider_type, so this is cosmetic for image generation.
func (p *ChatGPTOAuthRouter) ProviderType() string {
	return "chatgpt_oauth"
}
