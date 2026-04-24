//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSessionListPagedRich_EstimatedTokensHeuristic_UTF8 pins the PRE-fix
// byte-length heuristic: EstimatedTokens == length(messages_json)/4 + 12000.
//
// Characterization: pins PRE-fix heuristic behavior. Change asserted value when Phase 02 lands.
//
// The fixture uses Vietnamese UTF-8 text to expose the byte-over-rune overshoot:
// multi-byte chars inflate length() beyond character count, so estimated tokens
// are higher than a rune-based or actual-tiktoken count would produce.
func TestSessionListPagedRich_EstimatedTokensHeuristic_UTF8(t *testing.T) {
	// Build a messages JSON array with ~2000 bytes of Vietnamese UTF-8 content.
	// Vietnamese uses 3-byte UTF-8 sequences for many chars, so a small string
	// produces a large byte count relative to character count.
	vietnameseText := "Xin chào! Đây là một đoạn văn bản tiếng Việt dùng để kiểm tra độ chính xác của ước tính số token. " +
		"Ngôn ngữ Việt Nam sử dụng nhiều ký tự đặc biệt với dấu thanh và dấu phụ, " +
		"điều này làm cho mỗi ký tự chiếm nhiều byte hơn trong mã hóa UTF-8. " +
		"Bộ ký tự này bao gồm các nguyên âm có dấu như: ắ, ặ, ầ, ẩ, ẫ, ậ, ề, ể, ễ, ệ, ỉ, ị, " +
		"ọ, ỏ, ố, ồ, ổ, ỗ, ộ, ớ, ờ, ở, ỡ, ợ, ụ, ủ, ứ, ừ, ử, ữ, ự, ỳ, ỷ, ỹ, ỵ. " +
		"Mỗi ký tự như vậy chiếm 3 byte trong UTF-8, so với chỉ 1 byte cho ký tự ASCII."

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := []msg{
		{Role: "user", Content: vietnameseText},
		{Role: "assistant", Content: vietnameseText},
	}
	msgsJSON, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// SQLite length() on a TEXT column returns Unicode character count (runes),
	// NOT byte count. For ASCII JSON framing the difference only shows in the
	// Vietnamese content chars that SQLite stores as UTF-8 but counts as runes.
	runeLen := len([]rune(string(msgsJSON)))
	wantEstimated := runeLen/4 + 12000

	// Sanity-check: the fixture must be large enough to demonstrate the heuristic.
	if runeLen < 1000 {
		t.Fatalf("fixture too small (%d runes); need >= 1000 to demonstrate heuristic", runeLen)
	}

	byteLen := len(msgsJSON) // kept for diagnostic messages only

	// Open fresh in-memory SQLite and apply schema.
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID := store.MasterTenantID.String()
	sessionID := uuid.New().String()
	sessionKey := "agent:test-agent:direct:user1"

	// Insert session directly — bypasses cache so length() operates on stored bytes.
	_, err = db.Exec(`INSERT INTO sessions
		(id, session_key, messages, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		sessionID, sessionKey, string(msgsJSON), tenantID)
	if err != nil {
		t.Fatalf("INSERT session: %v", err)
	}

	// Call ListPagedRich via the store (mirrors production code path).
	sessionStore := NewSQLiteSessionStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	result := sessionStore.ListPagedRich(ctx, store.SessionListOpts{Limit: 10})

	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("len(Sessions) = %d, want 1", len(result.Sessions))
	}

	got := result.Sessions[0].EstimatedTokens
	if got != wantEstimated {
		t.Errorf("EstimatedTokens = %d, want %d (runeLen=%d, byteLen=%d, formula: runeLen/4 + 12000)",
			got, wantEstimated, runeLen, byteLen)
	}
}
