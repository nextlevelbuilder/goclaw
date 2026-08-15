package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

func agentBudgetFromContext(ctx context.Context) usagecaps.AgentBudget {
	return usagecaps.AgentBudget{
		ContextWindow: store.AgentContextWindowFromContext(ctx),
		MaxTokens:     store.AgentMaxTokensFromContext(ctx),
	}
}

// reserveToolLLMUsage guards and reserves a tool-internal model call whose full
// input already lives in the ChatRequest (text plus the fixed inline-media
// convention). The fixed local BudgetCounter counts the request itself.
func reserveToolLLMUsage(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest) (*usagecaps.Reservation, error) {
	return reserveToolLLMUsageWithMedia(ctx, svc, toolName, providerName, model, req, "", nil)
}

// reserveToolLLMUsageWithMedia guards a native-media tool call whose payload is
// sent OUT-OF-BAND (native provider JSON body or File API upload) and is thus
// invisible to the ChatRequest the counter would otherwise see. To keep the
// budget authority model/provider-independent, the fixed local BudgetCounter
// counts the standard-base64 representation of the raw bytes as one extra
// synthetic input message. That synthetic message is added ONLY to the
// guard/reservation copy — it is never transported, because native paths build
// their own provider payload from the raw bytes separately.
//
// mediaData must be the real bytes that will be sent. A native-media call whose
// payload cannot be buffered here (a streamed remote URL) must NOT route through
// this helper with nil data — use reserveToolLLMUsageUnverifiableMedia, which
// fails closed under an agent budget instead of undercounting.
func reserveToolLLMUsageWithMedia(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest, mediaMIME string, mediaData []byte) (*usagecaps.Reservation, error) {
	if mediaMIME != "" || len(mediaData) > 0 {
		req = appendNativeMediaBudget(req, mediaMIME, mediaData)
	}
	budget := agentBudgetFromContext(ctx)
	req = clampToolRequestMaxTokens(req, budget.MaxTokens)
	if guardErr := usagecaps.GuardContextWindow(req, providerName, model, "tool:"+toolName, budget); guardErr != nil {
		return nil, guardErr
	}
	if svc == nil {
		return nil, nil
	}
	return svc.Preflight(ctx, usagecaps.Request{
		TenantID:        store.TenantIDFromContext(ctx),
		AgentID:         store.AgentIDFromContext(ctx),
		ProviderName:    providerName,
		ModelID:         model,
		ReservationKey:  fmt.Sprintf("tool:%s:%s", toolName, uuid.NewString()),
		Messages:        req.Messages,
		MaxOutputTokens: budget.MaxTokens,
	})
}

// reserveToolLLMUsageUnverifiableMedia handles a native-media call whose payload
// cannot be counted before transport (e.g. a remote video streamed straight to
// the provider without buffering). The complete-input invariant cannot be proven
// for such a call, so under an agent budget it fails closed with an explicit
// streaming error rather than trusting a byte-size/Content-Length estimate.
// Without a propagated agent budget it falls through to the text path, which
// itself fails closed with an AgentBudgetWiringError — every tool LLM call is
// agent-scoped, so there is no path here that reaches transport uncounted.
func reserveToolLLMUsageUnverifiableMedia(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest) (*usagecaps.Reservation, error) {
	budget := agentBudgetFromContext(ctx)
	if budget.ContextWindow > 0 || budget.MaxTokens > 0 {
		return nil, fmt.Errorf(
			"tool:%s: cannot verify streamed native media against the agent context budget (no in-memory payload to count); refusing to send",
			toolName,
		)
	}
	return reserveToolLLMUsage(ctx, svc, toolName, providerName, model, req)
}

// appendNativeMediaBudget returns a copy of req with one extra synthetic user
// message carrying the media MIME and the standard-base64 encoding of the raw
// payload, so the fixed BudgetCounter counts the real out-of-band input. The
// original message slice is not mutated; the returned request is guard-only.
func appendNativeMediaBudget(req providers.ChatRequest, mime string, data []byte) providers.ChatRequest {
	var b strings.Builder
	b.WriteString(mime)
	if len(data) > 0 {
		b.WriteByte('\n')
		b.WriteString(base64.StdEncoding.EncodeToString(data))
	}
	msgs := make([]providers.Message, len(req.Messages), len(req.Messages)+1)
	copy(msgs, req.Messages)
	msgs = append(msgs, providers.Message{Role: "user", Content: b.String()})
	req.Messages = msgs
	return req
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
