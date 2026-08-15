package tracing

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUsageCatchUpStartHourRefreshesLatestClosedBucket(t *testing.T) {
	target := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	latest := target

	got := usageCatchUpStartHour(&latest, target)
	want := target.Add(-recentUsageRefreshWindow)
	if !got.Equal(want) {
		t.Fatalf("start hour = %s, want %s", got, want)
	}
}

func TestUsageCatchUpStartHourContinuesLargeBacklog(t *testing.T) {
	target := time.Date(2026, 7, 3, 6, 0, 0, 0, time.UTC)
	latest := target.Add(-12 * time.Hour)

	got := usageCatchUpStartHour(&latest, target)
	want := latest.Add(time.Hour)
	if !got.Equal(want) {
		t.Fatalf("start hour = %s, want %s", got, want)
	}
}

func TestUsageCatchUpStartHourWithoutSnapshotsComputesPreviousHourOnly(t *testing.T) {
	target := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)

	got := usageCatchUpStartHour(nil, target)
	if !got.Equal(target) {
		t.Fatalf("start hour = %s, want %s", got, target)
	}
}

// TestMergeTraceAndSpanRowsDedupesToolCallProviderModelRow guards against
// SQLSTATE 21000 ("ON CONFLICT DO UPDATE command cannot affect row a second
// time"): a span row that resolves to Provider=="" && Model=="" (e.g. from
// tool_call spans, which never carry provider/model) must be merged into the
// existing totals row for that (agent_id, channel) instead of appended as a
// second row sharing the same upsert conflict-target key.
func TestMergeTraceAndSpanRowsDedupesToolCallProviderModelRow(t *testing.T) {
	bucketStart := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	agentID := uuid.New()

	traceRows := []traceAggregate{
		{
			AgentID:       &agentID,
			Channel:       "web",
			RequestCount:  10,
			ErrorCount:    1,
			UniqueUsers:   3,
			InputTokens:   100,
			OutputTokens:  200,
			TotalCost:     1.5,
			ToolCallCount: 4,
			AvgDurationMS: 500,
		},
	}

	spanRows := []spanAggregate{
		// Simulates leftover/edge-case span row with empty provider/model
		// (e.g. tool_call-only spans) that would otherwise collide with the
		// totals row's conflict-target key.
		{
			AgentID:           &agentID,
			Channel:           "web",
			Provider:          "",
			Model:             "",
			LLMCallCount:      2,
			InputTokens:       10,
			OutputTokens:      20,
			TotalCost:         0.1,
			CacheReadTokens:   1,
			CacheCreateTokens: 2,
			ThinkingTokens:    3,
		},
		{
			AgentID:           &agentID,
			Channel:           "web",
			Provider:          "anthropic",
			Model:             "claude-sonnet-4",
			LLMCallCount:      5,
			InputTokens:       50,
			OutputTokens:      60,
			TotalCost:         0.9,
			CacheReadTokens:   4,
			CacheCreateTokens: 5,
			ThinkingTokens:    6,
		},
	}

	snapshots := mergeTraceAndSpanRows(bucketStart, traceRows, spanRows, nil, nil)

	var totalsRows []int
	for i, snap := range snapshots {
		if snap.AgentID != nil && *snap.AgentID == agentID && snap.Channel == "web" &&
			snap.Provider == "" && snap.Model == "" {
			totalsRows = append(totalsRows, i)
		}
	}

	if len(totalsRows) != 1 {
		t.Fatalf("expected exactly 1 totals row with Provider==\"\" && Model==\"\" for (agentID, web), got %d", len(totalsRows))
	}

	totals := snapshots[totalsRows[0]]
	if want := 2; totals.LLMCallCount != want {
		t.Fatalf("LLMCallCount = %d, want %d", totals.LLMCallCount, want)
	}
	if want := int64(10); totals.InputTokens != want {
		t.Fatalf("InputTokens = %d, want %d", totals.InputTokens, want)
	}
	if want := int64(20); totals.OutputTokens != want {
		t.Fatalf("OutputTokens = %d, want %d", totals.OutputTokens, want)
	}
	if want := 0.1; totals.TotalCost != want {
		t.Fatalf("TotalCost = %v, want %v", totals.TotalCost, want)
	}
	if want := int64(1); totals.CacheReadTokens != want {
		t.Fatalf("CacheReadTokens = %d, want %d", totals.CacheReadTokens, want)
	}
	if want := int64(2); totals.CacheCreateTokens != want {
		t.Fatalf("CacheCreateTokens = %d, want %d", totals.CacheCreateTokens, want)
	}
	if want := int64(3); totals.ThinkingTokens != want {
		t.Fatalf("ThinkingTokens = %d, want %d", totals.ThinkingTokens, want)
	}

	// The distinct provider/model detail row must still exist separately.
	var detailFound bool
	for _, snap := range snapshots {
		if snap.Provider == "anthropic" && snap.Model == "claude-sonnet-4" {
			detailFound = true
			if snap.LLMCallCount != 5 {
				t.Fatalf("detail row LLMCallCount = %d, want 5", snap.LLMCallCount)
			}
		}
	}
	if !detailFound {
		t.Fatal("expected detail row for provider=anthropic model=claude-sonnet-4")
	}
}
