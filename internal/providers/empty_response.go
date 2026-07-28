package providers

import "strings"

// EmptyResponseError reports a provider call that returned no error and no
// content of any kind — no text, no thinking, no tool calls, no images, and no
// usage accounting.
//
// This is not a statement about any one vendor. Every provider in this package
// builds its ChatResponse with a default FinishReason of "stop" (openai_chat.go,
// ollama.go, codex.go, anthropic_stream.go all do), so a stream that ends before
// delivering a single chunk — connection dropped mid-response, an error payload
// shaped in a way the provider's parser skips, an upstream that closes cleanly
// after accepting the request — is indistinguishable from a successful empty
// answer. The agent loop then treats that as the model's final word and
// substitutes "..." for the user.
//
// Rather than teach every provider to recognise its own vendor's failure shape,
// the invariant is stated once against the ChatResponse contract: a successful
// response must carry SOMETHING. This holds for providers that do not exist yet.
//
// Precedent: 339b13e8 fixed exactly this class of bug for Codex
// ("return error on response.failed instead of silent success"); this is the
// same judgement applied at the contract level instead of per vendor.
type EmptyResponseError struct {
	Provider string
	Model    string
}

func (e *EmptyResponseError) Error() string {
	var b strings.Builder
	b.WriteString("provider returned a successful response with no content, no tool calls and no usage")
	if e.Provider != "" || e.Model != "" {
		b.WriteString(" (")
		b.WriteString(e.Provider)
		if e.Model != "" {
			b.WriteString("/")
			b.WriteString(e.Model)
		}
		b.WriteString(")")
	}
	return b.String()
}

// ResponseCarriesNoSignal reports whether resp is devoid of every signal a
// provider can return.
//
// The condition is deliberately an AND over ALL signals, because each of these
// is a legitimate "empty" response on its own and must keep working:
//
//   - NO_REPLY / silent replies: the model deliberately declines to answer. Text
//     is present (IsSilentReply matches the token) and usage is reported.
//   - Tool-call-only turns: Content is empty by design, ToolCalls is not.
//   - stopAfterTool turns: Content is empty, usage is still reported.
//   - Thinking-only turns: text landed in Thinking rather than Content.
//
// Usage alone is enough to clear the check: a provider that accounted for tokens
// did receive and complete a generation, whatever it chose to emit.
func ResponseCarriesNoSignal(resp *ChatResponse) bool {
	if resp == nil {
		return false // a nil response with a nil error is handled by callers
	}
	if resp.Content != "" || resp.Thinking != "" {
		return false
	}
	if len(resp.ToolCalls) > 0 || len(resp.Images) > 0 {
		return false
	}
	if resp.Usage != nil {
		return false
	}
	// Anthropic tool-use passback and Codex phases are carried outside Content.
	if len(resp.RawAssistantContent) > 0 || resp.ThinkingSignature != "" || resp.Phase != "" {
		return false
	}
	return true
}
