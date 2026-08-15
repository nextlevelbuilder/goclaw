//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteUsageEventStoreRefreshRollupHourIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	eventStore := NewSQLiteUsageEventStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	bucket := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)

	if err := eventStore.InsertEvent(ctx, &store.UsageEvent{
		ID:           uuid.New(),
		TenantID:     store.MasterTenantID,
		EventTime:    bucket.Add(10 * time.Minute),
		EventType:    store.UsageEventTypeRuntimeToolCall,
		ResourceType: store.UsageResourceTypeRuntimeTool,
		ResourceName: "exec",
		ResourceID:   "exec",
		Source:       store.UsageSourceToolCall,
		Status:       "completed",
		InputTokens:  10,
		TotalTokens:  10,
		DurationMS:   50,
		CallCount:    1,
	}); err != nil {
		t.Fatalf("InsertEvent first: %v", err)
	}
	if err := eventStore.RefreshEventRollupHour(ctx, bucket); err != nil {
		t.Fatalf("Refresh first: %v", err)
	}

	if err := eventStore.InsertEvent(ctx, &store.UsageEvent{
		ID:           uuid.New(),
		TenantID:     store.MasterTenantID,
		EventTime:    bucket.Add(20 * time.Minute),
		EventType:    store.UsageEventTypeRuntimeToolCall,
		ResourceType: store.UsageResourceTypeRuntimeTool,
		ResourceName: "exec",
		ResourceID:   "exec",
		Source:       store.UsageSourceToolCall,
		Status:       "error",
		OutputTokens: 5,
		TotalTokens:  5,
		DurationMS:   150,
		CallCount:    1,
		ErrorCount:   1,
	}); err != nil {
		t.Fatalf("InsertEvent second: %v", err)
	}
	if err := eventStore.RefreshEventRollupHour(ctx, bucket); err != nil {
		t.Fatalf("Refresh second: %v", err)
	}
	if err := eventStore.RefreshEventRollupHour(ctx, bucket); err != nil {
		t.Fatalf("Refresh idempotent: %v", err)
	}

	summary, err := eventStore.GetEventSummary(ctx, store.UsageEventQuery{
		From:         bucket,
		To:           bucket.Add(time.Hour),
		ResourceType: store.UsageResourceTypeRuntimeTool,
	})
	if err != nil {
		t.Fatalf("GetEventSummary: %v", err)
	}
	if summary.Calls != 2 || summary.Errors != 1 {
		t.Fatalf("summary calls/errors = %d/%d, want 2/1", summary.Calls, summary.Errors)
	}
	if summary.TotalTokens != 15 {
		t.Fatalf("summary tokens = %d, want 15", summary.TotalTokens)
	}
	if summary.AvgDurationMS != 100 {
		t.Fatalf("summary avg duration = %d, want 100", summary.AvgDurationMS)
	}
}
