//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteSnapshotStoreGetBreakdownProviderModelKeepsProviderAndModel(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	snapshots := NewSQLiteSnapshotStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	bucket := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	if err := snapshots.UpsertSnapshots(ctx, []store.UsageSnapshot{
		{
			BucketHour:   bucket,
			Provider:     "openrouter",
			Model:        "openai/gpt-5.5",
			InputTokens:  1200,
			OutputTokens: 300,
			LLMCallCount: 2,
			TotalCost:    0.42,
		},
	}); err != nil {
		t.Fatalf("UpsertSnapshots: %v", err)
	}

	rows, err := snapshots.GetBreakdown(ctx, store.SnapshotQuery{
		From:    bucket.Add(-time.Hour),
		To:      bucket.Add(time.Hour),
		GroupBy: "provider_model",
	})
	if err != nil {
		t.Fatalf("GetBreakdown: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if got, want := rows[0].Key, "openrouter/openai/gpt-5.5"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	if got, want := rows[0].TotalCost, 0.42; got != want {
		t.Fatalf("total_cost = %v, want %v", got, want)
	}
}
