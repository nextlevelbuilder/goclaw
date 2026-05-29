package caps

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/usage/pricing"
)

const TraceMetadataKey = "usage_caps"

type TraceMetadata struct {
	Decision            string   `json:"decision"`
	Reason              string   `json:"reason,omitempty"`
	ReservationKey      string   `json:"reservation_key,omitempty"`
	ProviderName        string   `json:"provider_name,omitempty"`
	ProviderType        string   `json:"provider_type,omitempty"`
	ModelID             string   `json:"model_id,omitempty"`
	EstimatedTokens     int64    `json:"estimated_tokens"`
	EstimatedCostMicros int64    `json:"estimated_cost_micros"`
	ActualTokens        int64    `json:"actual_tokens"`
	ActualCostMicros    int64    `json:"actual_cost_micros"`
	ReconcileStatus     string   `json:"reconcile_status,omitempty"`
	PolicyCount         int      `json:"policy_count"`
	PolicyIDs           []string `json:"policy_ids,omitempty"`
}

func (r *Reservation) TraceMetadata() TraceMetadata {
	if r == nil {
		return TraceMetadata{}
	}
	decision := r.decision
	if decision == "" {
		if r.skipped {
			decision = store.UsageCapEventSkip
		} else {
			decision = store.UsageCapEventAllow
		}
	}
	policyCount := 0
	policyIDs := make([]string, 0)
	if r.result != nil {
		policyCount = len(r.result.Policies)
		for _, policy := range r.result.Policies {
			if policy.ID != uuid.Nil {
				policyIDs = append(policyIDs, policy.ID.String())
			}
		}
	}
	if r.blockedPolicyID != uuid.Nil {
		policyIDs = append(policyIDs, r.blockedPolicyID.String())
		if policyCount == 0 {
			policyCount = 1
		}
	}
	return TraceMetadata{
		Decision:            decision,
		Reason:              r.reason,
		ReservationKey:      r.key,
		ProviderName:        r.providerName,
		ProviderType:        r.providerType,
		ModelID:             r.modelID,
		EstimatedTokens:     r.usage.TotalTokens(),
		EstimatedCostMicros: r.estimatedCostMicros,
		ActualTokens:        r.actualTokens,
		ActualCostMicros:    r.actualCostMicros,
		ReconcileStatus:     r.reconcileStatus,
		PolicyCount:         policyCount,
		PolicyIDs:           policyIDs,
	}
}

func (m TraceMetadata) Empty() bool {
	return m.Decision == "" && m.Reason == "" && m.ReservationKey == "" &&
		m.ProviderName == "" && m.ProviderType == "" && m.ModelID == "" &&
		m.EstimatedTokens == 0 && m.EstimatedCostMicros == 0 &&
		m.ActualTokens == 0 && m.ActualCostMicros == 0 &&
		m.ReconcileStatus == "" && m.PolicyCount == 0 && len(m.PolicyIDs) == 0
}

func MergeTraceMetadata(existing json.RawMessage, entries []TraceMetadata) json.RawMessage {
	clean := make([]TraceMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.Empty() {
			clean = append(clean, entry)
		}
	}
	if len(clean) == 0 {
		return existing
	}
	payload := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &payload)
	}
	payload[TraceMetadataKey] = map[string]any{"attempts": clean}
	data, err := json.Marshal(payload)
	if err != nil {
		return existing
	}
	return json.RawMessage(data)
}

func skippedReservation(req Request, reason string) *Reservation {
	return &Reservation{
		skipped: true, decision: store.UsageCapEventSkip, reason: reason,
		providerName: req.ProviderName, modelID: req.ModelID,
	}
}

func skippedScopedReservation(req Request, scope store.UsageCapScope, reason string) *Reservation {
	r := skippedReservation(req, reason)
	r.providerType = scope.ProviderType
	r.modelID = scope.ModelID
	return r
}

func blockedReservation(req Request, scope store.UsageCapScope, key string, usage pricing.BillableUsage, costMicros int64, policyID uuid.UUID, reason string) *Reservation {
	return &Reservation{
		key: key, usage: usage, estimatedCostMicros: costMicros,
		decision: store.UsageCapEventBlock, reason: reason,
		blockedPolicyID: policyID,
		providerName:    req.ProviderName, providerType: scope.ProviderType, modelID: scope.ModelID,
	}
}
