package http

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestBuildCodexPoolActivitySeparatesDirectSelectionAndFailover(t *testing.T) {
	now := time.Date(2026, 3, 24, 20, 0, 0, 0, time.UTC)
	directA := providers.MergeChatGPTOAuthRoutingMetadata(nil, providers.ChatGPTOAuthRoutingEvidence{
		Strategy:         "round_robin",
		PoolProviders:    []string{"pool-a", "pool-b", "pool-c"},
		SelectedProvider: "pool-a",
		ServingProvider:  "pool-a",
		AttemptCount:     1,
	})
	directB := providers.MergeChatGPTOAuthRoutingMetadata(nil, providers.ChatGPTOAuthRoutingEvidence{
		Strategy:         "round_robin",
		PoolProviders:    []string{"pool-a", "pool-b", "pool-c"},
		SelectedProvider: "pool-b",
		ServingProvider:  "pool-b",
		AttemptCount:     1,
	})
	failoverToB := providers.MergeChatGPTOAuthRoutingMetadata(nil, providers.ChatGPTOAuthRoutingEvidence{
		Strategy:           "round_robin",
		PoolProviders:      []string{"pool-a", "pool-b", "pool-c"},
		SelectedProvider:   "pool-a",
		ServingProvider:    "pool-b",
		AttemptedProviders: []string{"pool-a", "pool-b"},
		FailoverProviders:  []string{"pool-b"},
		AttemptCount:       2,
	})

	providerCounts, recent := buildCodexPoolActivity([]string{"pool-a", "pool-b", "pool-c"}, []store.CodexPoolSpan{
		{
			SpanID:     uuid.New(),
			TraceID:    uuid.New(),
			StartedAt:  now,
			DurationMS: 450,
			Status:     "completed",
			Provider:   "pool-a",
			Model:      "gpt-5.4",
			Metadata:   directA,
		},
		{
			SpanID:     uuid.New(),
			TraceID:    uuid.New(),
			StartedAt:  now.Add(1 * time.Minute),
			DurationMS: 500,
			Status:     "completed",
			Provider:   "pool-b",
			Model:      "gpt-5.4",
			Metadata:   directB,
		},
		{
			SpanID:     uuid.New(),
			TraceID:    uuid.New(),
			StartedAt:  now.Add(2 * time.Minute),
			DurationMS: 620,
			Status:     "completed",
			Provider:   "pool-b",
			Model:      "gpt-5.4",
			Metadata:   failoverToB,
		},
	})

	if len(providerCounts) != 3 {
		t.Fatalf("len(providerCounts) = %d, want 3", len(providerCounts))
	}
	if providerCounts[0].ProviderName != "pool-a" || providerCounts[0].DirectSelectionCount != 2 || providerCounts[0].FailoverServeCount != 0 {
		t.Fatalf("pool-a counts = %#v", providerCounts[0])
	}
	if providerCounts[0].SuccessCount != 1 || providerCounts[0].FailureCount != 1 || providerCounts[0].ConsecutiveFailures != 1 {
		t.Fatalf("pool-a runtime = %#v", providerCounts[0])
	}
	if providerCounts[1].ProviderName != "pool-b" || providerCounts[1].DirectSelectionCount != 1 || providerCounts[1].FailoverServeCount != 1 {
		t.Fatalf("pool-b counts = %#v", providerCounts[1])
	}
	if providerCounts[1].SuccessCount != 2 || providerCounts[1].FailureCount != 0 || providerCounts[1].HealthState != "healthy" {
		t.Fatalf("pool-b runtime = %#v", providerCounts[1])
	}
	if providerCounts[2].ProviderName != "pool-c" || providerCounts[2].DirectSelectionCount != 0 || providerCounts[2].FailoverServeCount != 0 {
		t.Fatalf("pool-c counts = %#v", providerCounts[2])
	}
	if providerCounts[2].HealthState != "idle" || providerCounts[2].HealthScore != 0 {
		t.Fatalf("pool-c health = %#v", providerCounts[2])
	}
	if len(recent) != 3 {
		t.Fatalf("len(recent) = %d, want 3", len(recent))
	}
	last := recent[0]
	if !last.UsedFailover {
		t.Fatal("latest.UsedFailover = false, want true")
	}
	if last.SelectedProvider != "pool-a" || last.ProviderName != "pool-b" {
		t.Fatalf("latest routing = selected %q provider %q", last.SelectedProvider, last.ProviderName)
	}
	if last.AttemptCount != 2 {
		t.Fatalf("latest.AttemptCount = %d, want 2", last.AttemptCount)
	}
}

func TestBuildCodexPoolActivityTracksTerminalFailuresAcrossAttempts(t *testing.T) {
	now := time.Date(2026, 3, 24, 22, 0, 0, 0, time.UTC)
	failedAttempt := providers.MergeChatGPTOAuthRoutingMetadata(nil, providers.ChatGPTOAuthRoutingEvidence{
		Strategy:           "round_robin",
		PoolProviders:      []string{"pool-a", "pool-b"},
		SelectedProvider:   "pool-a",
		AttemptedProviders: []string{"pool-a", "pool-b"},
		AttemptCount:       2,
	})

	providerCounts, recent := buildCodexPoolActivity([]string{"pool-a", "pool-b"}, []store.CodexPoolSpan{
		{
			SpanID:     uuid.New(),
			TraceID:    uuid.New(),
			StartedAt:  now,
			DurationMS: 900,
			Status:     "error",
			Provider:   "pool-a",
			Model:      "gpt-5.4",
			Metadata:   failedAttempt,
		},
	})

	if len(recent) != 1 || recent[0].Status != "error" {
		t.Fatalf("recent = %#v", recent)
	}
	if recent[0].ProviderName != "" {
		t.Fatalf("recent[0].ProviderName = %q, want empty", recent[0].ProviderName)
	}
	if providerCounts[0].FailureCount != 1 || providerCounts[0].ConsecutiveFailures != 1 {
		t.Fatalf("pool-a failure stats = %#v", providerCounts[0])
	}
	if providerCounts[1].FailureCount != 1 || providerCounts[1].ConsecutiveFailures != 1 {
		t.Fatalf("pool-b failure stats = %#v", providerCounts[1])
	}
	if providerCounts[0].HealthState != "critical" || providerCounts[1].HealthState != "critical" {
		t.Fatalf("health states = %#v %#v", providerCounts[0], providerCounts[1])
	}
}
