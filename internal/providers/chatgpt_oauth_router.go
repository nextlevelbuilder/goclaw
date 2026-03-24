package providers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

const chatGPTOAuthStrategyRoundRobin = "round_robin"

// ChatGPTOAuthRouter routes a ChatGPT OAuth-backed agent across multiple
// authenticated Codex providers while keeping the agent's primary provider as
// the preferred/default account.
type ChatGPTOAuthRouter struct {
	tenantID            uuid.UUID
	registry            *Registry
	defaultProviderName string
	extraProviderNames  []string
	strategy            string

	mu   sync.Mutex
	next int
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
	candidates := p.availableProviders()
	if len(candidates) == 0 {
		return p.defaultProviderName
	}
	if p.strategy != chatGPTOAuthStrategyRoundRobin {
		return candidates[0].Name()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return candidates[p.next%len(candidates)].Name()
}

func (p *ChatGPTOAuthRouter) DefaultModel() string {
	candidates := p.availableProviders()
	if len(candidates) == 0 {
		return ""
	}
	if p.strategy != chatGPTOAuthStrategyRoundRobin {
		return candidates[0].DefaultModel()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return candidates[p.next%len(candidates)].DefaultModel()
}

func (p *ChatGPTOAuthRouter) SupportsThinking() bool { return true }

// HasAvailableProviders reports whether at least one authenticated Codex provider
// is currently available for this router.
func (p *ChatGPTOAuthRouter) HasAvailableProviders() bool {
	return len(p.availableProviders()) > 0
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
	ordered, err := p.orderedProviders()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i, provider := range ordered {
		resp, callErr := fn(provider)
		if callErr == nil {
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

func (p *ChatGPTOAuthRouter) orderedProviders() ([]Provider, error) {
	candidates := p.availableProviders()
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no authenticated chatgpt_oauth providers available")
	}
	if p.strategy != chatGPTOAuthStrategyRoundRobin || len(candidates) == 1 {
		return candidates, nil
	}

	p.mu.Lock()
	start := p.next % len(candidates)
	p.next = (p.next + 1) % len(candidates)
	p.mu.Unlock()

	ordered := make([]Provider, 0, len(candidates))
	ordered = append(ordered, candidates[start:]...)
	ordered = append(ordered, candidates[:start]...)
	return ordered, nil
}

func (p *ChatGPTOAuthRouter) availableProviders() []Provider {
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
