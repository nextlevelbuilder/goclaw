package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

const chatGPTOAuthStrategyRoundRobin = "round_robin"
const chatGPTOAuthStrategyPriorityOrder = "priority_order"

// Modality keys used to scope round-robin counters so that chat and image
// traffic rotate independently within the same pool. See registry.RoundRobinNext.
const (
	chatGPTOAuthModalityChat  = "chat"
	chatGPTOAuthModalityImage = "image"
)

// ChatGPTOAuthRouter routes a ChatGPT OAuth-backed agent across multiple
// authenticated Codex providers while keeping the agent's primary provider as
// the preferred/default account.
type ChatGPTOAuthRouter struct {
	tenantID            uuid.UUID
	registry            *Registry
	defaultProviderName string
	extraProviderNames  []string
	strategy            string
}

type chatGPTOAuthRouteCandidate struct {
	provider    Provider
	eligibility RouteEligibility
}

// NewChatGPTOAuthRouter creates a provider wrapper for agent-side ChatGPT OAuth routing.
func NewChatGPTOAuthRouter(
	tenantID uuid.UUID,
	registry *Registry,
	defaultProviderName string,
	strategy string,
	extraProviderNames []string,
) *ChatGPTOAuthRouter {
	return &ChatGPTOAuthRouter{
		tenantID:            tenantID,
		registry:            registry,
		defaultProviderName: defaultProviderName,
		extraProviderNames:  extraProviderNames,
		strategy:            strategy,
	}
}

func (p *ChatGPTOAuthRouter) Name() string {
	selection, err := p.orderedProviders(context.Background(), chatGPTOAuthModalityChat, false)
	if err != nil || len(selection) == 0 {
		return p.defaultProviderName
	}
	return selection[0].Name()
}

func (p *ChatGPTOAuthRouter) DefaultModel() string {
	selection, err := p.orderedProviders(context.Background(), chatGPTOAuthModalityChat, false)
	if err != nil || len(selection) == 0 {
		return ""
	}
	return selection[0].DefaultModel()
}

func (p *ChatGPTOAuthRouter) SupportsThinking() bool { return true }

func (p *ChatGPTOAuthRouter) HasRegisteredProviders() bool {
	return len(p.registeredProviders()) > 0
}

// HasAvailableProviders reports whether at least one registered Codex provider is
// route-eligible right now after auth/quota readiness filtering.
func (p *ChatGPTOAuthRouter) HasAvailableProviders() bool {
	_, err := p.orderedProviders(context.Background(), chatGPTOAuthModalityChat, false)
	return err == nil
}

func (p *ChatGPTOAuthRouter) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.call(ctx, func(provider Provider) (*ChatResponse, error) {
		return provider.Chat(ctx, req)
	})
}

func (p *ChatGPTOAuthRouter) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.call(ctx, func(provider Provider) (*ChatResponse, error) {
		return provider.ChatStream(ctx, req, onChunk)
	})
}

func (p *ChatGPTOAuthRouter) call(ctx context.Context, fn func(Provider) (*ChatResponse, error)) (*ChatResponse, error) {
	ordered, err := p.orderedProviders(ctx, chatGPTOAuthModalityChat, true)
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
	var lastErr error
	for i, provider := range ordered {
		if observation := ChatGPTOAuthRoutingObservationFromContext(ctx); observation != nil {
			observation.RecordAttempt(provider.Name())
		}
		resp, callErr := fn(provider)
		if callErr == nil {
			if observation := ChatGPTOAuthRoutingObservationFromContext(ctx); observation != nil {
				observation.RecordSuccess(provider.Name())
			}
			return resp, nil
		}
		lastErr = callErr
		if !IsRetryableError(callErr) || i == len(ordered)-1 {
			return nil, callErr
		}
		slog.Warn("chatgpt_oauth router failover",
			"from", provider.Name(),
			"to", ordered[i+1].Name(),
			"error", callErr,
		)
	}
	return nil, lastErr
}

func (p *ChatGPTOAuthRouter) orderedProviders(ctx context.Context, modality string, advance bool) ([]Provider, error) {
	candidates := p.routeCandidates(ctx)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no authenticated chatgpt_oauth providers available")
	}

	if p.strategy == chatGPTOAuthStrategyPriorityOrder {
		return p.priorityOrderedProviders(candidates)
	}

	healthy := make([]Provider, 0, len(candidates))
	unknown := make([]Provider, 0, len(candidates))
	blocked := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.eligibility.Class {
		case RouteEligibilityHealthy:
			healthy = append(healthy, candidate.provider)
		case RouteEligibilityBlocked:
			blocked = append(blocked, formatRouteBlockReason(candidate.provider.Name(), candidate.eligibility.Reason))
		default:
			unknown = append(unknown, candidate.provider)
		}
	}

	active := healthy
	fallback := unknown
	if len(active) == 0 {
		active = unknown
		fallback = nil
	}
	if len(active) == 0 {
		return nil, fmt.Errorf("no route-eligible chatgpt_oauth providers available: %s", strings.Join(blocked, ", "))
	}
	if p.strategy != chatGPTOAuthStrategyRoundRobin || len(active) == 1 {
		ordered := append([]Provider(nil), active...)
		ordered = append(ordered, fallback...)
		return ordered, nil
	}

	start := p.registry.RoundRobinNext(p.tenantID, p.defaultProviderName, modality, len(active), advance)

	ordered := make([]Provider, 0, len(active)+len(fallback))
	ordered = append(ordered, active[start:]...)
	ordered = append(ordered, active[:start]...)
	ordered = append(ordered, fallback...)
	return ordered, nil
}

func (p *ChatGPTOAuthRouter) priorityOrderedProviders(candidates []chatGPTOAuthRouteCandidate) ([]Provider, error) {
	ordered := make([]Provider, 0, len(candidates))
	blocked := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.eligibility.Class == RouteEligibilityBlocked {
			blocked = append(blocked, formatRouteBlockReason(candidate.provider.Name(), candidate.eligibility.Reason))
			continue
		}
		ordered = append(ordered, candidate.provider)
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("no route-eligible chatgpt_oauth providers available: %s", strings.Join(blocked, ", "))
	}
	return ordered, nil
}

func (p *ChatGPTOAuthRouter) routeCandidates(ctx context.Context) []chatGPTOAuthRouteCandidate {
	registered := p.registeredProviders()
	candidates := make([]chatGPTOAuthRouteCandidate, 0, len(registered))
	for _, provider := range registered {
		eligibility := RouteEligibility{Class: RouteEligibilityHealthy}
		if aware, ok := provider.(RouteEligibilityAware); ok {
			eligibility = aware.RouteEligibility(ctx)
			if eligibility.Class == "" {
				eligibility.Class = RouteEligibilityUnknown
			}
		}
		candidates = append(candidates, chatGPTOAuthRouteCandidate{
			provider:    provider,
			eligibility: eligibility,
		})
	}
	return candidates
}

func (p *ChatGPTOAuthRouter) registeredProviders() []Provider {
	if p.registry == nil {
		return nil
	}
	names := make([]string, 0, 1+len(p.extraProviderNames))
	seen := make(map[string]bool, 1+len(p.extraProviderNames))
	appendName := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	appendName(p.defaultProviderName)
	for _, name := range p.extraProviderNames {
		appendName(name)
	}

	providers := make([]Provider, 0, len(names))
	for _, name := range names {
		provider, err := p.registry.GetForTenant(p.tenantID, name)
		if err != nil {
			continue
		}
		if _, ok := provider.(*CodexProvider); !ok {
			continue
		}
		providers = append(providers, provider)
	}
	return providers
}

func formatRouteBlockReason(providerName, reason string) string {
	if reason == "" {
		return providerName
	}
	return providerName + ":" + reason
}
