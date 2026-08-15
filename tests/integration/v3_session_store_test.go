//go:build integration

package integration

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreSession_GetOrCreateAndSave(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]

	// First call — creates new session.
	data := ss.GetOrCreate(ctx, key)
	if data == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	if data.Key != key {
		t.Errorf("expected Key=%q, got %q", key, data.Key)
	}

	// Second call with same key — returns same.
	data2 := ss.GetOrCreate(ctx, key)
	if data2 == nil {
		t.Fatal("second GetOrCreate returned nil")
	}
	if data2.Key != key {
		t.Errorf("expected same Key=%q, got %q", key, data2.Key)
	}

	// Save to DB.
	if err := ss.Save(ctx, key); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify row in DB.
	var dbKey string
	err := db.QueryRow(
		"SELECT session_key FROM sessions WHERE session_key = $1 AND tenant_id = $2",
		key, tenantID,
	).Scan(&dbKey)
	if err != nil {
		t.Fatalf("DB verify: %v", err)
	}
	if dbKey != key {
		t.Errorf("DB session_key mismatch: got %q", dbKey)
	}
}

func TestStoreSession_MessageLifecycle(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	// Add 3 messages.
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "how are you"},
	}
	for _, m := range msgs {
		ss.AddMessage(ctx, key, m)
	}

	// Verify 3 in order.
	hist := ss.GetHistory(ctx, key)
	if len(hist) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(hist))
	}
	for i, m := range msgs {
		if hist[i].Content != m.Content {
			t.Errorf("msg[%d]: expected %q, got %q", i, m.Content, hist[i].Content)
		}
	}

	// TruncateHistory keepLast=1.
	ss.TruncateHistory(ctx, key, 1)
	hist2 := ss.GetHistory(ctx, key)
	if len(hist2) != 1 {
		t.Fatalf("after truncate: expected 1 message, got %d", len(hist2))
	}
	if hist2[0].Content != "how are you" {
		t.Errorf("truncate kept wrong message: %q", hist2[0].Content)
	}

	// Save and verify.
	if err := ss.Save(ctx, key); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestStoreSession_SummaryAndLabel(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	// SetSummary / GetSummary.
	ss.SetSummary(ctx, key, "test summary text")
	if got := ss.GetSummary(ctx, key); got != "test summary text" {
		t.Errorf("GetSummary: expected %q, got %q", "test summary text", got)
	}

	// SetLabel / GetLabel.
	ss.SetLabel(ctx, key, "my label")
	if got := ss.GetLabel(ctx, key); got != "my label" {
		t.Errorf("GetLabel: expected %q, got %q", "my label", got)
	}
}

func TestStoreSession_Reset(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)
	ss.AddMessage(ctx, key, providers.Message{Role: "user", Content: "hello"})
	ss.SetSummary(ctx, key, "some summary")

	// Reset clears messages and summary.
	ss.Reset(ctx, key)

	hist := ss.GetHistory(ctx, key)
	if len(hist) != 0 {
		t.Errorf("after Reset: expected 0 messages, got %d", len(hist))
	}
	if got := ss.GetSummary(ctx, key); got != "" {
		t.Errorf("after Reset: expected empty summary, got %q", got)
	}

	// Save and verify DB.
	if err := ss.Save(ctx, key); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var summary *string
	var msgCount int
	err := db.QueryRow(
		`SELECT summary, jsonb_array_length(messages) FROM sessions WHERE session_key = $1 AND tenant_id = $2`,
		key, tenantID,
	).Scan(&summary, &msgCount)
	if err != nil {
		t.Fatalf("DB verify: %v", err)
	}
	if summary != nil && *summary != "" {
		t.Errorf("DB summary not cleared: %q", *summary)
	}
	if msgCount != 0 {
		t.Errorf("DB messages not cleared: %d", msgCount)
	}
}

func TestStoreSession_Delete(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)
	if err := ss.Save(ctx, key); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Delete.
	if err := ss.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get should return nil.
	if got := ss.Get(ctx, key); got != nil {
		t.Errorf("after Delete: expected nil from Get, got non-nil")
	}

	// DB row should be gone.
	var count int
	db.QueryRow(
		"SELECT COUNT(*) FROM sessions WHERE session_key = $1 AND tenant_id = $2",
		key, tenantID,
	).Scan(&count)
	if count != 0 {
		t.Errorf("after Delete: expected 0 DB rows, got %d", count)
	}
}

func TestStoreSession_TokenAccumulation(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	ss.AccumulateTokens(ctx, key, 100, 50)
	ss.AccumulateTokens(ctx, key, 200, 100)

	// Verify via session data.
	data := ss.Get(ctx, key)
	if data == nil {
		t.Fatal("Get returned nil after AccumulateTokens")
	}
	if data.InputTokens != 300 {
		t.Errorf("InputTokens: expected 300, got %d", data.InputTokens)
	}
	if data.OutputTokens != 150 {
		t.Errorf("OutputTokens: expected 150, got %d", data.OutputTokens)
	}
}

func TestStoreSession_CompactionCounter(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	ss.IncrementCompaction(ctx, key)
	ss.IncrementCompaction(ctx, key)
	ss.IncrementCompaction(ctx, key)

	count := ss.GetCompactionCount(ctx, key)
	if count != 3 {
		t.Errorf("GetCompactionCount: expected 3, got %d", count)
	}
}

func TestStoreSession_MetadataRoundtrip(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	meta := map[string]string{
		"source":   "telegram",
		"chat_id":  "12345",
		"language": "en",
	}
	ss.SetSessionMetadata(ctx, key, meta)

	got := ss.GetSessionMetadata(ctx, key)
	if got == nil {
		t.Fatal("GetSessionMetadata returned nil")
	}
	for k, v := range meta {
		if got[k] != v {
			t.Errorf("metadata[%q]: expected %q, got %q", k, v, got[k])
		}
	}
}

func TestStoreSession_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ss := pg.NewPGSessionStore(db)

	key := "test-sess-" + uuid.New().String()[:8]

	// Create session in tenant A.
	ss.GetOrCreate(ctxA, key)
	if err := ss.Save(ctxA, key); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Get from tenant B — should return nil (different in-memory cache, different tenant scope).
	// Create a new store to ensure no cache leak.
	ss2 := pg.NewPGSessionStore(db)
	got := ss2.Get(ctxB, key)
	if got != nil {
		t.Errorf("tenant isolation broken: tenant B got session created by tenant A")
	}
}

func TestStoreSession_ConcurrentAddMessage(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "concurrent-sess-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			ss.AddMessage(ctx, key, providers.Message{
				Role:    "user",
				Content: fmt.Sprintf("message-%d", i),
			})
		}(i)
	}
	wg.Wait()

	hist := ss.GetHistory(ctx, key)
	if len(hist) != goroutines {
		t.Errorf("expected %d messages, got %d", goroutines, len(hist))
	}
}

func TestStoreSession_ConcurrentSave(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	key := "concurrent-save-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, key)

	// Add messages then race Save() calls
	for i := range 5 {
		ss.AddMessage(ctx, key, providers.Message{
			Role:    "user",
			Content: fmt.Sprintf("msg-%d", i),
		})
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errChan := make(chan error, goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			if err := ss.Save(ctx, key); err != nil {
				errChan <- err
			}
		}()
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent Save failed: %v", err)
	}

	// Verify session still intact
	var dbKey string
	err := db.QueryRow(
		"SELECT session_key FROM sessions WHERE session_key = $1 AND tenant_id = $2",
		key, tenantID,
	).Scan(&dbKey)
	if err != nil {
		t.Fatalf("DB verify after concurrent Save: %v", err)
	}
}
