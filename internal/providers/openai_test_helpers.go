package providers

// BuildRequestBodyForTest is a test-only export of buildRequestBody for
// integration smoke tests in the tests/integration package. NOT part of the
// public API - do not call from production code paths.
func (p *OpenAIProvider) BuildRequestBodyForTest(model string, req ChatRequest, stream bool) map[string]any {
	return p.buildRequestBody(model, req, stream)
}
