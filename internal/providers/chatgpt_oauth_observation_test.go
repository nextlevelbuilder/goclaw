package providers

import (
	"testing"
)

func TestChatGPTOAuthRoutingObservationRoundTrip(t *testing.T) {
	observation := NewChatGPTOAuthRoutingObservation()
	observation.SetPool("openai-codex", "round_robin", []string{"openai-codex", "codex-work"})
	observation.RecordAttempt("openai-codex")
	observation.RecordAttempt("codex-work")
	observation.RecordSuccess("codex-work")

	snapshot := observation.Snapshot()
	if snapshot.PoolOwnerProvider != "openai-codex" {
		t.Fatalf("PoolOwnerProvider = %q, want openai-codex", snapshot.PoolOwnerProvider)
	}
	if snapshot.Strategy != "round_robin" {
		t.Fatalf("Strategy = %q, want round_robin", snapshot.Strategy)
	}
	if snapshot.SelectedProvider != "openai-codex" {
		t.Fatalf("SelectedProvider = %q, want openai-codex", snapshot.SelectedProvider)
	}
	if snapshot.ServingProvider != "codex-work" {
		t.Fatalf("ServingProvider = %q, want codex-work", snapshot.ServingProvider)
	}
	if snapshot.AttemptCount != 2 {
		t.Fatalf("AttemptCount = %d, want 2", snapshot.AttemptCount)
	}

	metadata := MergeChatGPTOAuthRoutingMetadata(nil, snapshot)
	extracted := ExtractChatGPTOAuthRoutingEvidence(metadata)
	if extracted.SelectedProvider != snapshot.SelectedProvider {
		t.Fatalf("Extracted SelectedProvider = %q, want %q", extracted.SelectedProvider, snapshot.SelectedProvider)
	}
	if extracted.ServingProvider != snapshot.ServingProvider {
		t.Fatalf("Extracted ServingProvider = %q, want %q", extracted.ServingProvider, snapshot.ServingProvider)
	}
	if len(extracted.FailoverProviders) != 1 || extracted.FailoverProviders[0] != "codex-work" {
		t.Fatalf("FailoverProviders = %#v, want [codex-work]", extracted.FailoverProviders)
	}
}
