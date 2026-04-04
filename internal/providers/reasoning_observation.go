package providers

import (
	"context"
	"encoding/json"
)

const ReasoningMetadataKey = "reasoning"

type reasoningDecisionKey struct{}

func WithReasoningDecision(ctx context.Context, decision ReasoningDecision) context.Context {
	return context.WithValue(ctx, reasoningDecisionKey{}, &decision)
}

func ReasoningDecisionFromContext(ctx context.Context) *ReasoningDecision {
	decision, _ := ctx.Value(reasoningDecisionKey{}).(*ReasoningDecision)
	return decision
}

func MergeReasoningMetadata(existing json.RawMessage, decision ReasoningDecision) json.RawMessage {
	if !decision.HasObservation() {
		return existing
	}
	payload := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &payload)
	}
	payload[ReasoningMetadataKey] = decision
	data, err := json.Marshal(payload)
	if err != nil {
		return existing
	}
	return json.RawMessage(data)
}
