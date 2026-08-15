package caps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ChatOptions identifies a billable non-agent LLM call for usage-cap enforcement.
type ChatOptions struct {
	TenantID        uuid.UUID
	AgentID         uuid.UUID
	ProviderName    string
	ModelID         string
	ReservationKey  string
	Purpose         string
	MaxOutputTokens int
	// AgentContextWindow and AgentMaxTokens are the caller agent's configured
	// request budget. Both are required for an agent-scoped call.
	AgentContextWindow int
	AgentMaxTokens     int
}

// Chat wraps Provider.Chat with the same usage-cap preflight and reconciliation
// used by agent loops. A nil service intentionally falls back to direct calls
// for Lite/subscription-only runtimes.
func (s *Service) Chat(ctx context.Context, provider providers.Provider, req providers.ChatRequest, opts ChatOptions) (*providers.ChatResponse, error) {
	if provider == nil {
		return nil, errors.New("usage cap chat: provider is nil")
	}
	agentID := opts.AgentID
	if agentID == uuid.Nil {
		agentID = store.AgentIDFromContext(ctx)
	}
	budget := AgentBudget{
		ContextWindow: opts.AgentContextWindow,
		MaxTokens:     opts.AgentMaxTokens,
	}
	if budget.ContextWindow <= 0 {
		budget.ContextWindow = store.AgentContextWindowFromContext(ctx)
	}
	if budget.MaxTokens <= 0 {
		budget.MaxTokens = store.AgentMaxTokensFromContext(ctx)
	}
	agentScoped := agentID != uuid.Nil
	if agentScoped && !budget.valid() {
		// A call carrying an agent ID but no wired request budget is a
		// background/utility LLM call (e.g. vault.classify, vault.batch_summarize)
		// that runs OUTSIDE an agent turn, where no per-agent window/max_tokens is
		// propagated. Rather than fail closed — which silently degrades enrichment
		// to the extractive fallback — fill a safe default budget so the window
		// guard still protects the provider transport. Logged at WARN so a genuine
		// agent-run wiring regression remains observable instead of masked.
		budget = backgroundBudgetFallback(opts.Purpose, budget, opts)
	}
	if agentScoped {
		req = clampRequestMaxTokens(req, budget.MaxTokens)
	}
	if s == nil || s.store == nil {
		guardName := opts.ProviderName
		if guardName == "" {
			guardName = provider.Name()
		}
		if agentScoped {
			if guardErr := GuardContextWindow(req, guardName, req.Model, opts.Purpose, budget); guardErr != nil {
				return nil, guardErr
			}
		}
		return provider.Chat(ctx, req)
	}
	if fallback, ok := provider.(*providers.ModelFallbackProvider); ok {
		return fallback.ChatWithHook(ctx, req, func(callCtx context.Context, entry providers.FallbackCandidate, actualReq providers.ChatRequest) (providers.FallbackAfterCall, error) {
			callOpts := opts
			callOpts.ProviderName = entry.ProviderName
			if callOpts.ProviderName == "" && entry.Provider != nil {
				callOpts.ProviderName = entry.Provider.Name()
			}
			callOpts.ModelID = actualReq.Model
			callOpts.ReservationKey = ""
			if agentScoped {
				actualReq = clampRequestMaxTokens(actualReq, budget.MaxTokens)
				if guardErr := GuardContextWindow(actualReq, callOpts.ProviderName, actualReq.Model, opts.Purpose, budget); guardErr != nil {
					return nil, guardErr
				}
			}
			usageReq := s.chatRequest(callCtx, entry.Provider, actualReq, callOpts)
			scopedCtx := scopedRequestContext(callCtx, usageReq)
			reservation, err := s.Preflight(scopedCtx, usageReq)
			if err != nil {
				return nil, err
			}
			return func(resp *providers.ChatResponse, callErr error, _ providers.FallbackCallInfo) {
				if reservation != nil {
					reservation.Reconcile(scopedCtx, resp, callErr)
				}
			}, nil
		})
	}

	guardName := opts.ProviderName
	if guardName == "" {
		guardName = provider.Name()
	}
	if agentScoped {
		if guardErr := GuardContextWindow(req, guardName, req.Model, opts.Purpose, budget); guardErr != nil {
			return nil, guardErr
		}
	}
	usageReq := s.chatRequest(ctx, provider, req, opts)
	scopedCtx := scopedRequestContext(ctx, usageReq)
	reservation, err := s.Preflight(scopedCtx, usageReq)
	if err != nil {
		return nil, err
	}
	resp, err := provider.Chat(scopedCtx, req)
	if reservation != nil {
		reservation.Reconcile(scopedCtx, resp, err)
	}
	return resp, err
}

func (s *Service) chatRequest(ctx context.Context, provider providers.Provider, req providers.ChatRequest, opts ChatOptions) Request {
	tenantID := opts.TenantID
	if tenantID == uuid.Nil {
		tenantID = store.TenantIDFromContext(ctx)
	}
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	agentID := opts.AgentID
	if agentID == uuid.Nil {
		agentID = store.AgentIDFromContext(ctx)
	}
	providerName := opts.ProviderName
	if providerName == "" && provider != nil {
		providerName = provider.Name()
	}
	modelID := opts.ModelID
	if modelID == "" {
		modelID = req.Model
	}
	if modelID == "" && provider != nil {
		modelID = provider.DefaultModel()
	}
	return Request{
		TenantID:        tenantID,
		AgentID:         agentID,
		ProviderName:    providerName,
		ModelID:         modelID,
		ReservationKey:  reservationKey(opts),
		Messages:        req.Messages,
		MaxOutputTokens: maxOutputTokens(req, opts.MaxOutputTokens),
	}
}

func scopedRequestContext(ctx context.Context, req Request) context.Context {
	if req.TenantID != uuid.Nil && store.TenantIDFromContext(ctx) != req.TenantID {
		ctx = store.WithTenantID(ctx, req.TenantID)
	}
	if req.AgentID != uuid.Nil && store.AgentIDFromContext(ctx) != req.AgentID {
		ctx = store.WithAgentID(ctx, req.AgentID)
	}
	return ctx
}

func reservationKey(opts ChatOptions) string {
	if opts.ReservationKey != "" {
		return opts.ReservationKey
	}
	purpose := opts.Purpose
	if purpose == "" {
		purpose = "llm"
	}
	return fmt.Sprintf("%s:%s", purpose, uuid.NewString())
}

// defaultBackgroundContextWindow bounds background/utility LLM calls that carry
// an agent ID but no wired per-agent budget. It is intentionally conservative:
// large enough for enrichment prompts (classify/summarize send truncated docs),
// small enough that a runaway prompt is still caught by the window guard.
const defaultBackgroundContextWindow = 200_000

// defaultBackgroundMaxTokens is the output reserve used when a background call
// declares no max_tokens of its own.
const defaultBackgroundMaxTokens = 4_096

// backgroundBudgetFallback fills a safe budget for an agent-scoped call whose
// window/max_tokens were never wired (a background/utility call outside an agent
// turn). Any operator-provided value is preserved; only the missing halves are
// defaulted. The window guard still runs against the result, so an oversized
// request fails closed on real input rather than on missing wiring.
func backgroundBudgetFallback(purpose string, budget AgentBudget, opts ChatOptions) AgentBudget {
	out := budget
	if out.MaxTokens <= 0 {
		out.MaxTokens = opts.MaxOutputTokens
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = defaultBackgroundMaxTokens
	}
	if out.ContextWindow <= 0 {
		out.ContextWindow = defaultBackgroundContextWindow
	}
	// Keep the invariant satisfiable: never let the reserve meet/exceed the window.
	if out.MaxTokens >= out.ContextWindow {
		out.MaxTokens = out.ContextWindow / 2
	}
	slog.Warn("caps.background_budget_default",
		"purpose", purpose,
		"context_window", out.ContextWindow,
		"max_tokens", out.MaxTokens,
		"reason", "agent-scoped call missing wired budget; using safe default instead of fail-closed",
	)
	return out
}

func clampRequestMaxTokens(req providers.ChatRequest, agentMaxTokens int) providers.ChatRequest {
	if agentMaxTokens <= 0 {
		return req
	}
	if req.Options == nil {
		req.Options = map[string]any{}
	}
	// A request that declares no max_tokens is SET to the agent's max_tokens
	// rather than left to a provider default, so the window invariant holds for
	// the value actually sent. A declared value is clamped down when it exceeds
	// the agent cap (or is a non-positive explicit value).
	current, ok := maxOutputTokensDeclared(req.Options)
	if !ok || current <= 0 || current > agentMaxTokens {
		req.Options[providers.OptMaxTokens] = agentMaxTokens
	}
	return req
}

// maxOutputTokensDeclared reports the declared max_tokens and whether the option
// was present at all. The bool lets the clamp distinguish a genuinely missing
// option from an explicit value, so a caller that declared nothing is pinned to
// the agent's max_tokens.
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

func maxOutputTokens(req providers.ChatRequest, fallback int) int {
	if fallback <= 0 {
		fallback = 1024
	}
	if req.Options == nil {
		return fallback
	}
	v, ok := req.Options[providers.OptMaxTokens]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return fallback
	}
}
