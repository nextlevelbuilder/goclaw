package agent

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// rejectSignallessResponse converts a provider call that succeeded while
// returning nothing at all into a transient error.
//
// Why this lives at the agent loop rather than inside a provider: every provider
// constructs its ChatResponse with FinishReason defaulted to "stop", so a stream
// that ends before delivering a single chunk looks exactly like a model that
// chose to answer with nothing. Fixing that per vendor means fixing it nine
// times over — once per ChatStream implementation — and again for the next
// provider added. callProvider is the single point every LLM call in a user turn
// passes through, streaming or not, wrapped in model-fallback or not, so the
// invariant is asserted once here and holds for providers that do not exist yet.
//
// The consequence of NOT doing this is silent: err is nil, so no retry path
// engages, the run finalizer substitutes "..." for the empty content, and a
// workflow step settles as "completed" carrying a three-character deliverable.
// The trace records status=completed with a "stop" finish reason, which is why
// this failure mode had to be inferred from result lengths rather than read from
// a log.
//
// Returning an error instead makes three existing mechanisms work as designed:
// provider-level retry (IsRetryableError), model fallback (ClassifyHTTPError no
// longer yields FailoverUnknown, so runOrdered walks to the next candidate), and
// workflow step requeue (IsTransientRunFailure).
func (l *Loop) rejectSignallessResponse(
	resp *providers.ChatResponse,
	callErr error,
	attempt string,
	opts []spanOption,
) (*providers.ChatResponse, error) {
	if callErr != nil || !providers.ResponseCarriesNoSignal(resp) {
		return resp, callErr
	}
	model, provider := l.resolveSpan(opts)
	// Logged at warn with the identifying fields because the only prior evidence
	// of this failure was a short result string in the database.
	slog.Warn("llm: provider returned no content, no tool calls and no usage",
		"attempt", attempt,
		"provider", provider,
		"model", model,
		"finish_reason", resp.FinishReason,
		"agent", l.id)
	return nil, &providers.EmptyResponseError{Provider: provider, Model: model}
}
