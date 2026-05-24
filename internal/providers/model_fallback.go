package providers

import (
	"context"
)

// FallbackCandidate is one runtime provider/model fallback option.
type FallbackCandidate struct {
	ProviderName string
	Model        string
	Provider     Provider
}

// ModelFallbackProvider wraps a primary provider with ordered fallback
// provider/model candidates. The primary candidate is always tried first.
type ModelFallbackProvider struct {
	primary     FallbackCandidate
	fallbacks   []FallbackCandidate
	classifier  ErrorClassifier
	tracker     *CooldownTracker
	maxAttempts int
}

func NewModelFallbackProvider(primary FallbackCandidate, fallbacks []FallbackCandidate, maxAttempts int, cooldownEnabled bool) *ModelFallbackProvider {
	var tracker *CooldownTracker
	if cooldownEnabled {
		tracker = NewCooldownTracker(0)
	}
	return &ModelFallbackProvider{
		primary:     primary,
		fallbacks:   fallbacks,
		classifier:  NewDefaultClassifier(),
		tracker:     tracker,
		maxAttempts: maxAttempts,
	}
}

func (p *ModelFallbackProvider) PrimaryProvider() Provider {
	return p.primary.Provider
}

func (p *ModelFallbackProvider) Name() string {
	if p.primary.Provider != nil {
		return p.primary.Provider.Name()
	}
	return p.primary.ProviderName
}

func (p *ModelFallbackProvider) DefaultModel() string {
	if p.primary.Model != "" {
		return p.primary.Model
	}
	if p.primary.Provider != nil {
		return p.primary.Provider.DefaultModel()
	}
	return ""
}

func (p *ModelFallbackProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		return entry.Provider.Chat(ctx, nextReq)
	})
}

func (p *ModelFallbackProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		streamed := false
		resp, err := entry.Provider.ChatStream(ctx, nextReq, func(chunk StreamChunk) {
			if chunk.Content != "" || chunk.Thinking != "" || len(chunk.Images) > 0 {
				streamed = true
			}
			onChunk(chunk)
		})
		if streamed && err != nil {
			return nil, noFallbackAfterStreamError{err: err}
		}
		return resp, err
	})
}

func (p *ModelFallbackProvider) runOrdered(
	ctx context.Context,
	req ChatRequest,
	call func(context.Context, FallbackCandidate, ChatRequest) (*ChatResponse, error),
) (*ChatResponse, error) {
	candidates := p.orderedCandidates(req.Model)
	var attempts []FailoverAttempt
	for i, entry := range candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if p.maxAttempts > 0 && i >= p.maxAttempts {
			break
		}
		key := CooldownKey(entry.ProviderName, entry.Model)
		if p.tracker != nil && !p.tracker.IsAvailable(key) && !p.tracker.ShouldProbe(key) {
			continue
		}
		resp, err := call(ctx, entry, req)
		if err == nil {
			if p.tracker != nil {
				p.tracker.RecordSuccess(key)
			}
			return resp, nil
		}
		if streamErr, ok := err.(noFallbackAfterStreamError); ok {
			return nil, streamErr.err
		}
		classification := ClassifyHTTPError(p.classifier, err)
		attempts = append(attempts, FailoverAttempt{
			Candidate:      ModelCandidate{Provider: entry.ProviderName, Model: entry.Model, ProfileID: entry.ProviderName + "/" + entry.Model},
			Classification: classification,
			Err:            err,
		})
		if p.tracker != nil && classification.Kind == "reason" {
			p.tracker.RecordFailure(key, classification.Reason)
		}
		if classification.Kind == "context_overflow" || classification.Reason == FailoverUnknown {
			return nil, err
		}
	}
	return nil, &FailoverSummaryError{Attempts: attempts}
}

func (p *ModelFallbackProvider) orderedCandidates(requestModel string) []FallbackCandidate {
	primary := p.primary
	if requestModel != "" {
		primary.Model = requestModel
	}
	out := []FallbackCandidate{primary}
	for _, fallback := range p.fallbacks {
		if fallback.Provider == nil || fallback.ProviderName == "" || fallback.Model == "" {
			continue
		}
		if fallback.ProviderName == primary.ProviderName && fallback.Model == primary.Model {
			continue
		}
		out = append(out, fallback)
	}
	return out
}

type noFallbackAfterStreamError struct {
	err error
}

func (e noFallbackAfterStreamError) Error() string {
	return e.err.Error()
}
