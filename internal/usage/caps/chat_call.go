package caps

import (
	"context"
	"errors"
	"fmt"

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
}

// Chat wraps Provider.Chat with the same usage-cap preflight and reconciliation
// used by agent loops. A nil service intentionally falls back to direct calls
// for Lite/subscription-only runtimes.
func (s *Service) Chat(ctx context.Context, provider providers.Provider, req providers.ChatRequest, opts ChatOptions) (*providers.ChatResponse, error) {
	if provider == nil {
		return nil, errors.New("usage cap chat: provider is nil")
	}
	if s == nil || s.store == nil {
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
