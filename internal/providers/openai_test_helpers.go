package providers

// BuildRequestBodyForTest exposes buildRequestBody for integration smoke tests.
// Not part of the public API - guarded by debug builds is unnecessary because
// the function is read-only and side-effect-free.
func (p *OpenAIProvider) BuildRequestBodyForTest(model string, req ChatRequest, stream bool) map[string]any {
	return p.buildRequestBody(model, req, stream)
}
