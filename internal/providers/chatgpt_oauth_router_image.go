package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// compile-time assertion: ChatGPTOAuthRouter satisfies NativeImageProvider.
var _ NativeImageProvider = (*ChatGPTOAuthRouter)(nil)

// GenerateImage implements NativeImageProvider for ChatGPTOAuthRouter.
// It iterates the strategy-ordered pool members, delegating to each member's
// GenerateImage in turn. Failover semantics mirror the Chat/call() path:
//   - retryable error (IsRetryableError) → try next member
//   - non-retryable error → return immediately
//   - all members exhausted → return aggregated error naming every attempted member
//
// Round-robin state advances once per GenerateImage call (via orderedProviders
// advance=true), regardless of which member ultimately serves the response.
// This matches the Chat path semantics documented on call().
func (p *ChatGPTOAuthRouter) GenerateImage(ctx context.Context, req NativeImageRequest) (*NativeImageResult, error) {
	ordered, err := p.orderedProviders(ctx, true)
	if err != nil {
		return nil, err
	}

	if observation := ChatGPTOAuthRoutingObservationFromContext(ctx); observation != nil {
		poolProviders := make([]string, 0, len(p.registeredProviders()))
		for _, provider := range p.registeredProviders() {
			poolProviders = append(poolProviders, provider.Name())
		}
		observation.SetPool(p.defaultProviderName, p.strategy, poolProviders)
	}

	attempted := make([]string, 0, len(ordered))
	var lastErr error

	for i, provider := range ordered {
		// Check context before attempting each member so a pre-cancelled ctx is
		// caught even when orderedProviders returns without error.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		np, ok := provider.(NativeImageProvider)
		if !ok {
			slog.Warn("chatgpt_oauth router image: member has no native image support, skipping",
				"provider", provider.Name(),
			)
			lastErr = fmt.Errorf("member %s has no native image support", provider.Name())
			attempted = append(attempted, provider.Name())
			continue
		}

		if observation := ChatGPTOAuthRoutingObservationFromContext(ctx); observation != nil {
			observation.RecordAttempt(provider.Name())
		}

		attempted = append(attempted, provider.Name())
		res, callErr := np.GenerateImage(ctx, req)
		if callErr == nil {
			if observation := ChatGPTOAuthRoutingObservationFromContext(ctx); observation != nil {
				observation.RecordSuccess(provider.Name())
			}
			return res, nil
		}

		lastErr = callErr

		// Non-retryable error: surface immediately without trying further members.
		if !IsRetryableError(callErr) {
			return nil, callErr
		}

		// Retryable: log and continue to the next member if one exists.
		if i < len(ordered)-1 {
			slog.Warn("chatgpt_oauth router image failover",
				"from", provider.Name(),
				"to", ordered[i+1].Name(),
				"error", callErr,
			)
		}
	}

	// All members exhausted (or none implemented NativeImageProvider).
	return nil, fmt.Errorf("all pool members failed image generation (%s): %w",
		strings.Join(attempted, ", "), lastErr)
}
