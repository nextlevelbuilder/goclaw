//go:build integration

package invariants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// INVARIANT: Session A's message history MUST NOT leak to Session B.
func TestSessionBoundary_MessageHistory(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	ss := pg.NewPGSessionStore(db)
	sessionA := "inv-sess-a-" + uuid.New().String()[:8]
	sessionB := "inv-sess-b-" + uuid.New().String()[:8]

	// Create messages in session A
	ss.GetOrCreate(ctx, sessionA)
	ss.AddMessage(ctx, sessionA, providers.Message{Role: "user", Content: "secret message A"})
	ss.AddMessage(ctx, sessionA, providers.Message{Role: "assistant", Content: "response A"})
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	// Create session B with different messages
	ss.GetOrCreate(ctx, sessionB)
	ss.AddMessage(ctx, sessionB, providers.Message{Role: "user", Content: "message B"})
	if err := ss.Save(ctx, sessionB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// INVARIANT: Session A's history must be separate from session B
	histA := ss.GetHistory(ctx, sessionA)
	histB := ss.GetHistory(ctx, sessionB)

	if len(histA) != 2 {
		t.Errorf("session A should have 2 messages, got %d", len(histA))
	}
	if len(histB) != 1 {
		t.Errorf("session B should have 1 message, got %d", len(histB))
	}

	// INVARIANT: Content must not leak between sessions
	for _, m := range histB {
		if m.Content == "secret message A" || m.Content == "response A" {
			t.Errorf("INVARIANT VIOLATION: session A's message leaked to session B: %q", m.Content)
		}
	}
}

// INVARIANT: Session A's summary MUST NOT leak to Session B.
func TestSessionBoundary_Summary(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	ss := pg.NewPGSessionStore(db)
	sessionA := "inv-sess-sum-a-" + uuid.New().String()[:8]
	sessionB := "inv-sess-sum-b-" + uuid.New().String()[:8]

	// Set summary in session A
	ss.GetOrCreate(ctx, sessionA)
	ss.SetSummary(ctx, sessionA, "secret summary for session A")
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	// Create session B without summary
	ss.GetOrCreate(ctx, sessionB)
	if err := ss.Save(ctx, sessionB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// INVARIANT: Session B MUST NOT see session A's summary
	summaryB := ss.GetSummary(ctx, sessionB)
	if summaryB == "secret summary for session A" {
		t.Errorf("INVARIANT VIOLATION: session A's summary leaked to session B")
	}
	if summaryB != "" {
		t.Errorf("session B should have empty summary, got %q", summaryB)
	}

	// Verify session A still has its summary
	summaryA := ss.GetSummary(ctx, sessionA)
	if summaryA != "secret summary for session A" {
		t.Errorf("session A's summary should persist, got %q", summaryA)
	}
}

// INVARIANT: Session A's metadata MUST NOT leak to Session B.
func TestSessionBoundary_Metadata(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	ss := pg.NewPGSessionStore(db)
	sessionA := "inv-sess-meta-a-" + uuid.New().String()[:8]
	sessionB := "inv-sess-meta-b-" + uuid.New().String()[:8]

	// Set metadata in session A
	ss.GetOrCreate(ctx, sessionA)
	ss.SetSessionMetadata(ctx, sessionA, map[string]string{
		"secret_key": "secret_value",
		"user_data":  "sensitive",
	})
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	// Create session B with different metadata
	ss.GetOrCreate(ctx, sessionB)
	ss.SetSessionMetadata(ctx, sessionB, map[string]string{
		"channel": "telegram",
	})
	if err := ss.Save(ctx, sessionB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// INVARIANT: Session B MUST NOT see session A's metadata
	metaB := ss.GetSessionMetadata(ctx, sessionB)
	if metaB["secret_key"] != "" {
		t.Errorf("INVARIANT VIOLATION: session A's metadata leaked to session B: secret_key=%q", metaB["secret_key"])
	}
	if metaB["user_data"] != "" {
		t.Errorf("INVARIANT VIOLATION: session A's metadata leaked to session B: user_data=%q", metaB["user_data"])
	}

	// Verify session B has its own metadata
	if metaB["channel"] != "telegram" {
		t.Errorf("session B should have its own metadata, got channel=%q", metaB["channel"])
	}

	// Verify session A still has its metadata
	metaA := ss.GetSessionMetadata(ctx, sessionA)
	if metaA["secret_key"] != "secret_value" {
		t.Errorf("session A's metadata should persist, got secret_key=%q", metaA["secret_key"])
	}
}

// INVARIANT: Session A's label MUST NOT leak to Session B.
func TestSessionBoundary_Label(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	ss := pg.NewPGSessionStore(db)
	sessionA := "inv-sess-label-a-" + uuid.New().String()[:8]
	sessionB := "inv-sess-label-b-" + uuid.New().String()[:8]

	// Set label in session A
	ss.GetOrCreate(ctx, sessionA)
	ss.SetLabel(ctx, sessionA, "Private Conversation")
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	// Create session B without label
	ss.GetOrCreate(ctx, sessionB)
	if err := ss.Save(ctx, sessionB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// INVARIANT: Session B MUST NOT see session A's label
	labelB := ss.GetLabel(ctx, sessionB)
	if labelB == "Private Conversation" {
		t.Errorf("INVARIANT VIOLATION: session A's label leaked to session B")
	}
}

// INVARIANT: Session A's token counts MUST NOT affect Session B.
func TestSessionBoundary_TokenCounts(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	ss := pg.NewPGSessionStore(db)
	sessionA := "inv-sess-tokens-a-" + uuid.New().String()[:8]
	sessionB := "inv-sess-tokens-b-" + uuid.New().String()[:8]

	// Accumulate tokens in session A
	ss.GetOrCreate(ctx, sessionA)
	ss.AccumulateTokens(ctx, sessionA, 1000, 500)
	ss.AccumulateTokens(ctx, sessionA, 2000, 1000)
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	// Create session B
	ss.GetOrCreate(ctx, sessionB)
	if err := ss.Save(ctx, sessionB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// Verify session A has accumulated tokens
	dataA := ss.Get(ctx, sessionA)
	if dataA.InputTokens != 3000 || dataA.OutputTokens != 1500 {
		t.Errorf("session A tokens: expected 3000/1500, got %d/%d", dataA.InputTokens, dataA.OutputTokens)
	}

	// INVARIANT: Session B MUST NOT have session A's token counts
	dataB := ss.Get(ctx, sessionB)
	if dataB.InputTokens != 0 || dataB.OutputTokens != 0 {
		t.Errorf("INVARIANT VIOLATION: session B has tokens %d/%d, should be 0/0",
			dataB.InputTokens, dataB.OutputTokens)
	}
}

// INVARIANT: Resetting Session A MUST NOT affect Session B.
func TestSessionBoundary_ResetIsolation(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	ss := pg.NewPGSessionStore(db)
	sessionA := "inv-sess-reset-a-" + uuid.New().String()[:8]
	sessionB := "inv-sess-reset-b-" + uuid.New().String()[:8]

	// Set up both sessions with data
	ss.GetOrCreate(ctx, sessionA)
	ss.AddMessage(ctx, sessionA, providers.Message{Role: "user", Content: "message A"})
	ss.SetSummary(ctx, sessionA, "summary A")
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save A: %v", err)
	}

	ss.GetOrCreate(ctx, sessionB)
	ss.AddMessage(ctx, sessionB, providers.Message{Role: "user", Content: "message B"})
	ss.SetSummary(ctx, sessionB, "summary B")
	if err := ss.Save(ctx, sessionB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// Reset session A
	ss.Reset(ctx, sessionA)
	if err := ss.Save(ctx, sessionA); err != nil {
		t.Fatalf("Save after reset: %v", err)
	}

	// INVARIANT: Session B MUST NOT be affected by session A's reset
	histB := ss.GetHistory(ctx, sessionB)
	if len(histB) != 1 {
		t.Errorf("INVARIANT VIOLATION: session B lost messages after session A reset, got %d", len(histB))
	}
	if histB[0].Content != "message B" {
		t.Errorf("session B should keep its message, got %q", histB[0].Content)
	}

	summaryB := ss.GetSummary(ctx, sessionB)
	if summaryB != "summary B" {
		t.Errorf("INVARIANT VIOLATION: session B lost summary after session A reset, got %q", summaryB)
	}

	// Verify session A was actually reset
	histA := ss.GetHistory(ctx, sessionA)
	if len(histA) != 0 {
		t.Errorf("session A should have 0 messages after reset, got %d", len(histA))
	}
}
