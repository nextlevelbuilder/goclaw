package providers

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
)

const ChatGPTOAuthRoutingMetadataKey = "chatgpt_oauth_routing"

type chatGPTOAuthRoutingObservationKey struct{}

type ChatGPTOAuthRoutingEvidence struct {
	PoolOwnerProvider  string   `json:"pool_owner_provider,omitempty"`
	Strategy           string   `json:"strategy,omitempty"`
	PoolProviders      []string `json:"pool_providers,omitempty"`
	SelectedProvider   string   `json:"selected_provider,omitempty"`
	ServingProvider    string   `json:"serving_provider,omitempty"`
	AttemptedProviders []string `json:"attempted_providers,omitempty"`
	FailoverProviders  []string `json:"failover_providers,omitempty"`
	AttemptCount       int      `json:"attempt_count,omitempty"`
}

func (e ChatGPTOAuthRoutingEvidence) HasData() bool {
	return e.SelectedProvider != "" || e.ServingProvider != "" || e.AttemptCount > 0 || len(e.PoolProviders) > 0
}

type ChatGPTOAuthRoutingObservation struct {
	mu       sync.Mutex
	evidence ChatGPTOAuthRoutingEvidence
}

func NewChatGPTOAuthRoutingObservation() *ChatGPTOAuthRoutingObservation {
	return &ChatGPTOAuthRoutingObservation{}
}

func WithChatGPTOAuthRoutingObservation(ctx context.Context, observation *ChatGPTOAuthRoutingObservation) context.Context {
	return context.WithValue(ctx, chatGPTOAuthRoutingObservationKey{}, observation)
}

func ChatGPTOAuthRoutingObservationFromContext(ctx context.Context) *ChatGPTOAuthRoutingObservation {
	observation, _ := ctx.Value(chatGPTOAuthRoutingObservationKey{}).(*ChatGPTOAuthRoutingObservation)
	return observation
}

func (o *ChatGPTOAuthRoutingObservation) SetPool(poolOwnerProvider, strategy string, poolProviders []string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evidence.PoolOwnerProvider = poolOwnerProvider
	o.evidence.Strategy = strategy
	o.evidence.PoolProviders = append([]string(nil), poolProviders...)
}

func (o *ChatGPTOAuthRoutingObservation) RecordAttempt(providerName string) {
	if o == nil || providerName == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evidence.AttemptCount++
	if o.evidence.SelectedProvider == "" {
		o.evidence.SelectedProvider = providerName
	}
	if !slices.Contains(o.evidence.AttemptedProviders, providerName) {
		o.evidence.AttemptedProviders = append(o.evidence.AttemptedProviders, providerName)
	}
	if o.evidence.SelectedProvider != providerName && !slices.Contains(o.evidence.FailoverProviders, providerName) {
		o.evidence.FailoverProviders = append(o.evidence.FailoverProviders, providerName)
	}
}

func (o *ChatGPTOAuthRoutingObservation) RecordSuccess(providerName string) {
	if o == nil || providerName == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evidence.ServingProvider = providerName
}

func (o *ChatGPTOAuthRoutingObservation) Snapshot() ChatGPTOAuthRoutingEvidence {
	if o == nil {
		return ChatGPTOAuthRoutingEvidence{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	evidence := o.evidence
	evidence.PoolProviders = append([]string(nil), o.evidence.PoolProviders...)
	evidence.AttemptedProviders = append([]string(nil), o.evidence.AttemptedProviders...)
	evidence.FailoverProviders = append([]string(nil), o.evidence.FailoverProviders...)
	return evidence
}

func MergeChatGPTOAuthRoutingMetadata(existing json.RawMessage, evidence ChatGPTOAuthRoutingEvidence) json.RawMessage {
	if !evidence.HasData() {
		return existing
	}
	payload := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &payload)
	}
	payload[ChatGPTOAuthRoutingMetadataKey] = evidence
	data, err := json.Marshal(payload)
	if err != nil {
		return existing
	}
	return json.RawMessage(data)
}

func ExtractChatGPTOAuthRoutingEvidence(raw json.RawMessage) ChatGPTOAuthRoutingEvidence {
	if len(raw) == 0 {
		return ChatGPTOAuthRoutingEvidence{}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ChatGPTOAuthRoutingEvidence{}
	}
	section := payload[ChatGPTOAuthRoutingMetadataKey]
	if len(section) == 0 {
		return ChatGPTOAuthRoutingEvidence{}
	}
	var evidence ChatGPTOAuthRoutingEvidence
	if err := json.Unmarshal(section, &evidence); err != nil {
		return ChatGPTOAuthRoutingEvidence{}
	}
	evidence.PoolProviders = uniqueStrings(evidence.PoolProviders)
	evidence.AttemptedProviders = uniqueStrings(evidence.AttemptedProviders)
	evidence.FailoverProviders = uniqueStrings(evidence.FailoverProviders)
	return evidence
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || slices.Contains(result, value) {
			continue
		}
		result = append(result, value)
	}
	return result
}
