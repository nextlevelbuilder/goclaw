package providers

import (
	"context"
	"sort"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// FallbackCandidate is one runtime provider/model fallback option.
type FallbackCandidate struct {
	ProviderName string
	Model        string
	Provider     Provider
}

// FallbackPolicy is the per-agent policy applied to fallback selection.
// Strategy "" means the provider default (priority order); known strategies
// are the "priority_order" / "health_order" values normalized by the store
// layer. MinAttemptsForHealth is the minimum number of observed attempts
// before a health score is allowed to drive re-ordering (0 = provider default).
type FallbackPolicy struct {
	Strategy            string
	MinAttemptsForHealth int
}

// ModelFallbackProvider wraps a primary provider with ordered fallback
// provider/model candidates. The primary candidate is always tried first.
type ModelFallbackProvider struct {
	primary     FallbackCandidate
	fallbacks   []FallbackCandidate
	classifier  ErrorClassifier
	tracker     *CooldownTracker
	maxAttempts int
	policy      FallbackPolicy
	// diagnostics records every candidate decision made by runOrdered (tried or
	// skipped) for operator visibility. Best-effort: populated under an internal
	// mutex so concurrent runs never race, but a snapshot may interleave two
	// concurrent runs' entries. Single-run use (the common case) is exact.
	diagnostics   []attemptDiagnostic
	diagnosticsMu sync.Mutex
}

// attemptDiagnostic records one decision runOrdered made for a candidate.
// Skipped candidates were not tried; SkipReason says why ("cooldown").
// HealthScore is the candidate's runtime reliability score at ordering time,
// or -1 when no health registry is available.
type attemptDiagnostic struct {
	Candidate   FallbackCandidate
	Skipped     bool
	SkipReason  string
	HealthScore float64
}

// healthScoreUnknown marks a diagnostic whose health score was not available
// at ordering time (registry absent or nil).
const healthScoreUnknown = -1

// LastAttempts returns a copy of the decisions recorded by the most recent
// runOrdered execution. The copy makes it safe to hold onto while another run
// mutates the provider. Entries interleave across concurrent runs; single-run
// callers see an exact per-run record.
func (p *ModelFallbackProvider) LastAttempts() []attemptDiagnostic {
	p.diagnosticsMu.Lock()
	defer p.diagnosticsMu.Unlock()
	out := make([]attemptDiagnostic, len(p.diagnostics))
	copy(out, p.diagnostics)
	return out
}

func (p *ModelFallbackProvider) recordDiagnostic(d attemptDiagnostic) {
	p.diagnosticsMu.Lock()
	defer p.diagnosticsMu.Unlock()
	p.diagnostics = append(p.diagnostics, d)
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

// WithFallbackPolicy attaches the fallback selection policy to the provider.
// It returns p for chaining and is nil-safe.
func (p *ModelFallbackProvider) WithFallbackPolicy(policy FallbackPolicy) *ModelFallbackProvider {
	if p == nil {
		return nil
	}
	p.policy = policy
	return p
}

// Policy returns the fallback policy currently attached to the provider.
func (p *ModelFallbackProvider) Policy() FallbackPolicy {
	if p == nil {
		return FallbackPolicy{}
	}
	return p.policy
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
			p.recordDiagnostic(attemptDiagnostic{
				Candidate:   entry,
				Skipped:     true,
				SkipReason:  "cooldown",
				HealthScore: healthScoreFor(entry),
			})
			continue
		}
		p.recordDiagnostic(attemptDiagnostic{
			Candidate:   entry,
			HealthScore: healthScoreFor(entry),
		})
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

// minHealthAttemptsFor returns the attempt threshold that qualifies a
// candidate's health score for re-ordering. Under the health_order policy an
// explicit threshold wins; the package default applies otherwise (and all
// non-health_order strategies keep the provider default exactly).
func (p *ModelFallbackProvider) minHealthAttemptsFor() int {
	if p.policy.Strategy == FallbackStrategyHealth && p.policy.MinAttemptsForHealth > 0 {
		return p.policy.MinAttemptsForHealth
	}
	return healthScoreMinAttempts
}

// orderByHealth re-ranks nonzero fallback candidates by their runtime
// reliability score. The primary candidate is ALWAYS first — the caller's
// explicit choice wins over runtime heuristics. For the remaining candidates,
// those with a meaningful health signal (at least minHealthAttemptsFor
// observed attempts) are sorted by descending HealthRegistry.Score; candidates
// without signal keep their configured order and trail the scored ones. With
// the "health_order" policy, the same ranking is applied but candidates below
// the attempt threshold — regardless of their provisional score — keep
// configured order after the qualified ones. This preserves existing behavior
// for new deployments (no signal yet → configured order) while steering away
// from provably flaky fallbacks.
func (p *ModelFallbackProvider) orderByHealth(candidates []FallbackCandidate) []FallbackCandidate {
	if len(candidates) < 3 {
		return candidates
	}
	reg := reliability.Default()
	if reg == nil || reg.Health == nil {
		return candidates
	}
	rest := candidates[1:]
	minAttempts := p.minHealthAttemptsFor()
	qualified := func(c FallbackCandidate) bool {
		return reg.Health.Status(c.ProviderName, c.Model).Attempts >= minAttempts
	}
	scoreOf := func(c FallbackCandidate) (float64, bool) {
		if !qualified(c) {
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

// healthScoreMinAttempts is the package-default minimum number of observed
// attempts before a health score is considered meaningful enough to drive
// fallback ordering. A FallbackPolicy.MinAttemptsForHealth > 0 overrides it.
const healthScoreMinAttempts = 5

// healthScoreFor returns the candidate's runtime reliability score, or
// healthScoreUnknown when no registry is available (fresh process).
func healthScoreFor(c FallbackCandidate) float64 {
	reg := reliability.Default()
	if reg == nil || reg.Health == nil {
		return healthScoreUnknown
	}
	return reg.Health.Score(c.ProviderName, c.Model)
}

// defaultFallbackPolicy is the process-wide fallback policy used by operators
// that build wrappers without an explicit WithFallbackPolicy. Read-only view
// via DefaultFallbackPolicyView; SetDefaultFallbackPolicy replaces it. The
// zero value keeps the provider default (priority order, 5 attempts).
var defaultFallbackPolicy FallbackPolicy

// SetDefaultFallbackPolicy replaces the process-wide fallback policy for
// wrappers that carry no explicit policy.
func SetDefaultFallbackPolicy(p FallbackPolicy) {
	defaultFallbackPolicy = p
}

// DefaultFallbackPolicyView returns a copy of the process-wide fallback policy,
// nil-safe and safe to mutate by the caller.
func DefaultFallbackPolicyView() FallbackPolicy {
	return defaultFallbackPolicy
}

// Known normalized fallback strategy values.
const (
	// FallbackStrategyPriority keeps configured order, trailing scored ones.
	FallbackStrategyPriority = "priority_order"
	// FallbackStrategyHealth ranks qualified candidates by health score.
	FallbackStrategyHealth = "health_order"
)

type noFallbackAfterStreamError struct {
	err error
}

func (e noFallbackAfterStreamError) Error() string {
	return e.err.Error()
}
