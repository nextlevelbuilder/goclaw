package providers

import (
	"context"
	"sort"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
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

type FallbackCallInfo struct {
	Streamed bool
}

type FallbackAfterCall func(*ChatResponse, error, FallbackCallInfo)
type FallbackBeforeCall func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (after FallbackAfterCall, err error)

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

func (p *ModelFallbackProvider) ChatWithHook(ctx context.Context, req ChatRequest, before FallbackBeforeCall) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		after, err := before(ctx, entry, nextReq)
		if err != nil {
			return nil, err
		}
		resp, err := entry.Provider.Chat(ctx, nextReq)
		if after != nil {
			after(resp, err, FallbackCallInfo{})
		}
		return resp, err
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

func (p *ModelFallbackProvider) ChatStreamWithHook(ctx context.Context, req ChatRequest, onChunk func(StreamChunk), before FallbackBeforeCall) (*ChatResponse, error) {
	return p.runOrdered(ctx, req, func(ctx context.Context, entry FallbackCandidate, req ChatRequest) (*ChatResponse, error) {
		nextReq := req
		nextReq.Model = entry.Model
		after, err := before(ctx, entry, nextReq)
		if err != nil {
			return nil, err
		}
		streamed := false
		resp, err := entry.Provider.ChatStream(ctx, nextReq, func(chunk StreamChunk) {
			if chunk.Content != "" || chunk.Thinking != "" || len(chunk.Images) > 0 {
				streamed = true
			}
			onChunk(chunk)
		})
		if after != nil {
			after(resp, err, FallbackCallInfo{Streamed: streamed})
		}
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
	return p.orderByHealth(out)
}

// orderByHealth re-ranks nonzero fallback candidates by their runtime
// reliability score. The primary candidate is ALWAYS first — the caller's
// explicit choice wins over runtime heuristics. For the remaining candidates,
// those with a meaningful health signal (more than scoreMinAttempts observed
// attempts) are sorted by descending HealthRegistry.Score; candidates without
// signal keep their configured order and trail the scored ones. This preserves
// existing behavior for new deployments (no signal yet → configured order)
// while steering away from provably flaky fallbacks.
func (p *ModelFallbackProvider) orderByHealth(candidates []FallbackCandidate) []FallbackCandidate {
	if len(candidates) < 3 {
		return candidates
	}
	reg := reliability.Default()
	if reg == nil || reg.Health == nil {
		return candidates
	}
	rest := candidates[1:]
	scoreOf := func(c FallbackCandidate) (float64, bool) {
		s := reg.Health.Status(c.ProviderName, c.Model)
		if s.Attempts <= healthScoreMinAttempts {
			return 0, false // no signal yet — keep configured position
		}
		return reg.Health.Score(c.ProviderName, c.Model), true
	}
	var scored, unscored []FallbackCandidate
	for _, c := range rest {
		if _, ok := scoreOf(c); ok {
			scored = append(scored, c)
		} else {
			unscored = append(unscored, c)
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		si, _ := scoreOf(scored[i])
		sj, _ := scoreOf(scored[j])
		return si > sj
	})
	out := make([]FallbackCandidate, 0, len(candidates))
	out = append(out, candidates[0])
	out = append(out, scored...)
	out = append(out, unscored...)
	return out
}

// healthScoreMinAttempts is the minimum number of observed attempts before a
// health score is considered meaningful enough to drive fallback ordering.
const healthScoreMinAttempts = 5

type noFallbackAfterStreamError struct {
	err error
}

func (e noFallbackAfterStreamError) Error() string {
	return e.err.Error()
}
