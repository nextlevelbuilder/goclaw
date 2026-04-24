//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSessionListPagedRich_MetadataTokensPreferredOverHeuristic verifies that when
// last_prompt_tokens is persisted in metadata, ListPagedRich returns that value
// for EstimatedTokens instead of the byte-length heuristic.
func TestSessionListPagedRich_MetadataTokensPreferredOverHeuristic(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	sessionStore := NewSQLiteSessionStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	const sessionKey = "agent:test-agent:direct:user-meta-test"
	const wantTokens = 50000
	const wantMsgCount = 620

	// Create session in cache.
	sessionStore.GetOrCreate(ctx, sessionKey)

	// Set last prompt tokens (in-memory only until Save).
	sessionStore.SetLastPromptTokens(ctx, sessionKey, wantTokens, wantMsgCount)

	// Save — this should persist the values into metadata JSON.
	if err := sessionStore.Save(ctx, sessionKey); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// ListPagedRich should prefer the metadata value over the heuristic.
	result := sessionStore.ListPagedRich(ctx, store.SessionListOpts{Limit: 10})
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("len(Sessions) = %d, want 1", len(result.Sessions))
	}

	got := result.Sessions[0].EstimatedTokens
	if got != wantTokens {
		t.Errorf("EstimatedTokens = %d, want %d (should use metadata, not heuristic)", got, wantTokens)
	}
}

// TestSessionLoadFromDB_RestoresLastPromptTokens verifies that after evicting the
// in-memory cache and reloading from DB (simulating a server restart), GetLastPromptTokens
// returns the value previously persisted into metadata.
func TestSessionLoadFromDB_RestoresLastPromptTokens(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	sessionStore := NewSQLiteSessionStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	const sessionKey = "agent:test-agent:direct:user-reload-test"
	const wantTokens = 50000
	const wantMsgCount = 620

	// Create, set tokens, save.
	sessionStore.GetOrCreate(ctx, sessionKey)
	sessionStore.SetLastPromptTokens(ctx, sessionKey, wantTokens, wantMsgCount)
	if err := sessionStore.Save(ctx, sessionKey); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Evict cache to simulate server restart.
	sessionStore.mu.Lock()
	delete(sessionStore.cache, sessionCacheKey(ctx, sessionKey))
	sessionStore.mu.Unlock()

	// Re-read via Get — triggers loadFromDB.
	reloaded := sessionStore.Get(ctx, sessionKey)
	if reloaded == nil {
		t.Fatal("Get after cache eviction returned nil")
	}

	// Verify GetLastPromptTokens returns the persisted value.
	gotTokens, gotMsgCount := sessionStore.GetLastPromptTokens(ctx, sessionKey)
	if gotTokens != wantTokens {
		t.Errorf("GetLastPromptTokens tokens = %d, want %d after reload", gotTokens, wantTokens)
	}
	if gotMsgCount != wantMsgCount {
		t.Errorf("GetLastPromptTokens msgCount = %d, want %d after reload", gotMsgCount, wantMsgCount)
	}
}

// TestSessionListPagedRich_FallsBackToHeuristicWhenNoMetadataTokens verifies the
// Phase 01 characterization test behavior: sessions with no last_prompt_tokens in
// metadata still use the heuristic (COALESCE fallback path).
func TestSessionListPagedRich_FallsBackToHeuristicWhenNoMetadataTokens(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	sessionStore := NewSQLiteSessionStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	const sessionKey = "agent:test-agent:direct:user-heuristic-test"

	// Create session and save WITHOUT setting last_prompt_tokens.
	sessionStore.GetOrCreate(ctx, sessionKey)
	if err := sessionStore.Save(ctx, sessionKey); err != nil {
		t.Fatalf("Save: %v", err)
	}

	result := sessionStore.ListPagedRich(ctx, store.SessionListOpts{Limit: 10})
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}

	// Empty messages JSON = "[]" = 2 bytes; length("[]") = 2 in SQLite (ASCII-only)
	// heuristic: 2/4 + 12000 = 12000.
	got := result.Sessions[0].EstimatedTokens
	if got != 12000 {
		t.Errorf("EstimatedTokens = %d, want 12000 (heuristic fallback for empty session)", got)
	}
}

// TestSessionListPagedRich_ZeroTokensDoesNotWriteMetadata verifies that a session
// with LastPromptTokens == 0 does NOT write last_prompt_tokens into metadata,
// preserving the COALESCE fallback to heuristic.
func TestSessionListPagedRich_ZeroTokensDoesNotWriteMetadata(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	sessionStore := NewSQLiteSessionStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	const sessionKey = "agent:test-agent:direct:user-zero-tokens-test"
	sessionID := uuid.New().String()

	// Insert session via raw SQL with custom metadata to verify it isn't overwritten.
	_, err := db.Exec(`INSERT INTO sessions
		(id, session_key, messages, metadata, tenant_id, created_at, updated_at)
		VALUES (?, ?, '[]', '{"custom_key":"custom_value"}', ?, datetime('now'), datetime('now'))`,
		sessionID, sessionKey, store.MasterTenantID.String())
	if err != nil {
		t.Fatalf("INSERT session: %v", err)
	}

	// Load into cache and save with zero LastPromptTokens — should NOT touch metadata.
	data := sessionStore.GetOrCreate(ctx, sessionKey)
	// data.LastPromptTokens is already 0 from loadFromDB (no key in metadata)
	_ = data
	if err := sessionStore.Save(ctx, sessionKey); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify custom_key preserved and last_prompt_tokens absent.
	meta := sessionStore.GetSessionMetadata(ctx, sessionKey)
	if meta["custom_key"] != "custom_value" {
		t.Errorf("custom_key = %q, want %q", meta["custom_key"], "custom_value")
	}
	if _, hasKey := meta["last_prompt_tokens"]; hasKey {
		t.Errorf("metadata unexpectedly contains last_prompt_tokens when LastPromptTokens==0")
	}
}
