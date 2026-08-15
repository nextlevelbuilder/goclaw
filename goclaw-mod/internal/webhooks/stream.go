package webhooks

// DefaultStream is the fallback for the webhook internal-streaming flag when
// config is unset. Streaming is on by default so the upstream provider can
// populate/serve its prompt cache for server-side webhook agent runs (sync,
// async, and test). Non-streaming requests are not cached by some
// OpenAI-compatible routers, so webhook runs would otherwise always pay full
// input-token price even with a stable session.
const DefaultStream = true

// ResolveStream converts the optional gateway.webhook_stream config into a bool:
// nil → DefaultStream (true); otherwise the explicit value.
func ResolveStream(v *bool) bool {
	if v == nil {
		return DefaultStream
	}
	return *v
}
