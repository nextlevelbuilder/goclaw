package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/mediabudget"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// budgetRefusalError marks a denial that came from the budget, not from a
// provider being unwell. A media fallback chain must stop on one: trying the
// next provider only moves the same payload to another endpoint, and the next
// entry may not price media at all.
type budgetRefusalError struct{ err error }

func (e *budgetRefusalError) Error() string { return e.err.Error() }
func (e *budgetRefusalError) Unwrap() error { return e.err }

// asBudgetRefusal marks err when it is one of the budget denials, and returns
// it untouched otherwise. Every gate on the media path funnels through here:
// the unmeasurable-media refusal, the agent context window, the missing-budget
// wiring failure, and the tenant usage cap.
func asBudgetRefusal(err error) error {
	if err == nil {
		return nil
	}
	var windowErr *usagecaps.ContextWindowExceededError
	var wiringErr *usagecaps.AgentBudgetWiringError
	switch {
	case errors.Is(err, mediabudget.ErrDurationUnmeasurable),
		errors.Is(err, usagecaps.ErrCapExceeded),
		errors.As(err, &windowErr),
		errors.As(err, &wiringErr):
		return &budgetRefusalError{err: err}
	default:
		return err
	}
}

// isBudgetRefusal reports whether err is a budget denial, however deeply it has
// been wrapped on its way back up.
func isBudgetRefusal(err error) bool {
	var refusal *budgetRefusalError
	return errors.As(err, &refusal)
}

func agentBudgetFromContext(ctx context.Context) usagecaps.AgentBudget {
	return usagecaps.AgentBudget{
		ContextWindow: store.AgentContextWindowFromContext(ctx),
		MaxTokens:     store.AgentMaxTokensFromContext(ctx),
	}
}

// reserveToolLLMUsage guards and reserves a tool-internal model call whose full
// input already lives in the ChatRequest. The fixed local BudgetCounter counts
// the request itself.
func reserveToolLLMUsage(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest) (*usagecaps.Reservation, error) {
	return reserveToolLLMUsageWithMediaTokens(ctx, svc, toolName, providerName, model, req, 0)
}

// reserveToolLLMUsageWithMedia guards a native-media tool call whose payload is
// sent OUT-OF-BAND (native provider JSON body or File API upload) and is thus
// invisible to the ChatRequest the counter would otherwise see. A payload
// mediabudget cannot price aborts the call rather than being waved through at
// an invented number.
func reserveToolLLMUsageWithMedia(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest, media ...mediabudget.Payload) (*usagecaps.Reservation, error) {
	mediaTokens := 0
	for _, p := range media {
		tokens, err := mediabudget.Tokens(ctx, p)
		if err != nil {
			return nil, asBudgetRefusal(err)
		}
		mediaTokens += tokens
	}
	return reserveToolLLMUsageWithMediaTokens(ctx, svc, toolName, providerName, model, req, mediaTokens)
}

// refuseUnpriceableMedia is the front gate for a payload whose cost cannot be
// established at all, used where there is not even a Payload to hand to
// mediabudget. It carries the same terminal marker as a priced refusal.
func refuseUnpriceableMedia(kind mediabudget.Kind, cause error) error {
	return asBudgetRefusal(fmt.Errorf("%s budget: %w", kind, cause))
}

// reserveToolLLMUsageWithMediaTokens is the shared path for a media charge that is already a token count, not a mediabudget.Payload.
func reserveToolLLMUsageWithMediaTokens(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest, mediaTokens int) (*usagecaps.Reservation, error) {
	budget := agentBudgetFromContext(ctx)
	req = clampToolRequestMaxTokens(req, budget.MaxTokens)
	if guardErr := usagecaps.GuardContextWindowWithMediaTokens(req, providerName, model, "tool:"+toolName, budget, mediaTokens); guardErr != nil {
		return nil, asBudgetRefusal(guardErr)
	}
	if svc == nil {
		return nil, nil
	}
	reservation, err := svc.Preflight(ctx, usagecaps.Request{
		TenantID:         store.TenantIDFromContext(ctx),
		AgentID:          store.AgentIDFromContext(ctx),
		ProviderName:     providerName,
		ModelID:          model,
		ReservationKey:   fmt.Sprintf("tool:%s:%s", toolName, uuid.NewString()),
		Messages:         req.Messages,
		MaxOutputTokens:  budget.MaxTokens,
		ExtraInputTokens: mediaTokens,
	})
	return reservation, asBudgetRefusal(err)
}

// clampToolRequestMaxTokens enforces max_tokens <= agentMaxTokens for every
// agent-originated tool call. A request that does not declare max_tokens is set
// to the agent's max_tokens rather than left to a provider default, so the
// window invariant (completeInput + agentMaxTokens <= window) holds for the
// value actually sent.
func clampToolRequestMaxTokens(req providers.ChatRequest, agentMaxTokens int) providers.ChatRequest {
	if agentMaxTokens <= 0 {
		return req
	}
	if req.Options == nil {
		req.Options = map[string]any{}
	}
	current, ok := maxOutputTokensDeclared(req.Options)
	if !ok || current > agentMaxTokens {
		req.Options[providers.OptMaxTokens] = agentMaxTokens
	}
	return req
}

// maxOutputTokensDeclared reports the max_tokens declared in the request options
// and whether it was present at all. The bool distinguishes a genuinely missing
// option (ok == false) from an explicit zero, which the clamp needs so it can
// set the agent's max_tokens when the caller declared nothing.
func maxOutputTokensDeclared(options map[string]any) (int, bool) {
	if options == nil {
		return 0, false
	}
	v, ok := options[providers.OptMaxTokens]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

func maxOutputTokensFromOptions(options map[string]any) int {
	maxTokens, _ := maxOutputTokensDeclared(options)
	return maxTokens
}
