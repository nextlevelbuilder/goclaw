//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// seedPendingGroup buffers n group messages for a fresh tenant and returns the
// store, a tenant-scoped context, and the messages in chronological order.
func seedPendingGroup(t *testing.T, historyKey string, n int) (*pg.PGPendingMessageStore, context.Context, []store.PendingMessage) {
	t.Helper()
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	base := time.Now().Add(-time.Duration(n) * time.Minute).UTC().Truncate(time.Millisecond)
	msgs := make([]store.PendingMessage, n)
	for i := range msgs {
		msgs[i] = store.PendingMessage{
			ID:            uuid.Must(uuid.NewV7()),
			ChannelName:   "dangzalo",
			HistoryKey:    historyKey,
			Sender:        "Diệu Hương",
			SenderID:      "uid-1",
			Body:          "raw message " + uuid.NewString(),
			PlatformMsgID: uuid.NewString(),
			CreatedAt:     base.Add(time.Duration(i) * time.Minute),
		}
	}

	s := pg.NewPGPendingMessageStore(db)
	if err := s.AppendBatch(ctx, msgs); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	// AppendBatch stamps server time on every row, so a batch shares one
	// created_at. Spread them to exercise ordering and the `since` filter.
	for _, m := range msgs {
		if _, err := db.Exec(
			`UPDATE channel_pending_messages SET created_at = $1 WHERE id = $2`, m.CreatedAt, m.ID,
		); err != nil {
			t.Fatalf("spread created_at: %v", err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM channel_message_archive WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM channel_pending_messages WHERE tenant_id = $1", tenantID)
	})
	return s, ctx, msgs
}

// Handing the buffer to the agent (bot mentioned) must preserve the raw group
// messages: the buffer used to be their only stored copy.
func TestPGDeleteByKeyArchivesMessages(t *testing.T) {
	historyKey := "group-" + uuid.NewString()
	s, ctx, msgs := seedPendingGroup(t, historyKey, 3)

	if err := s.DeleteByKey(ctx, "dangzalo", historyKey); err != nil {
		t.Fatalf("DeleteByKey: %v", err)
	}

	live, err := s.ListByKey(ctx, "dangzalo", historyKey)
	if err != nil {
		t.Fatalf("ListByKey: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("buffer has %d rows after clear, want 0", len(live))
	}

	archived, err := s.ListArchivedByKey(ctx, "dangzalo", historyKey, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	if len(archived) != len(msgs) {
		t.Fatalf("archived %d rows, want %d", len(archived), len(msgs))
	}
	bodies := make(map[string]string, len(archived))
	for _, a := range archived {
		bodies[a.Body] = a.ArchiveReason
	}
	for _, m := range msgs {
		reason, ok := bodies[m.Body]
		if !ok {
			t.Fatalf("message %q missing from archive", m.Body)
		}
		if reason != store.ArchiveReasonConsumed {
			t.Fatalf("archive reason = %q, want %q", reason, store.ArchiveReasonConsumed)
		}
	}
}

// Compaction summarizes old messages with an LLM and drops the originals. The
// summary is lossy, so the raw rows must land in the archive.
func TestPGCompactArchivesReplacedMessages(t *testing.T) {
	historyKey := "group-" + uuid.NewString()
	s, ctx, msgs := seedPendingGroup(t, historyKey, 5)

	summary := &store.PendingMessage{
		ChannelName: "dangzalo",
		HistoryKey:  historyKey,
		Sender:      "[summary]",
		Body:        "Concise summary of the first three messages.",
		IsSummary:   true,
	}
	if err := s.Compact(ctx, []uuid.UUID{msgs[0].ID, msgs[1].ID, msgs[2].ID}, summary); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	live, err := s.ListByKey(ctx, "dangzalo", historyKey)
	if err != nil {
		t.Fatalf("ListByKey: %v", err)
	}
	if len(live) != 3 { // 2 kept raw + 1 summary
		t.Fatalf("buffer has %d rows after compaction, want 3", len(live))
	}

	archived, err := s.ListArchivedByKey(ctx, "dangzalo", historyKey, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	if len(archived) != 3 {
		t.Fatalf("archived %d rows, want 3", len(archived))
	}
	for _, a := range archived {
		if a.ArchiveReason != store.ArchiveReasonCompacted {
			t.Fatalf("archive reason = %q, want %q", a.ArchiveReason, store.ArchiveReasonCompacted)
		}
	}
	// `since` must filter on the original capture time, not the archive time.
	recent, err := s.ListArchivedByKey(ctx, "dangzalo", historyKey, msgs[2].CreatedAt, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey(since): %v", err)
	}
	if len(recent) != 1 || recent[0].Body != msgs[2].Body {
		t.Fatalf("since filter returned %d rows, want only the newest compacted message", len(recent))
	}
}

// Compact then clear must archive every raw row exactly once.
func TestPGArchiveIsIdempotentAcrossCompactThenClear(t *testing.T) {
	historyKey := "group-" + uuid.NewString()
	s, ctx, msgs := seedPendingGroup(t, historyKey, 4)

	summary := &store.PendingMessage{
		ChannelName: "dangzalo",
		HistoryKey:  historyKey,
		Sender:      "[summary]",
		Body:        "Summary.",
		IsSummary:   true,
	}
	if err := s.Compact(ctx, []uuid.UUID{msgs[0].ID, msgs[1].ID}, summary); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := s.DeleteByKey(ctx, "dangzalo", historyKey); err != nil {
		t.Fatalf("DeleteByKey: %v", err)
	}
	if err := s.DeleteByKey(ctx, "dangzalo", historyKey); err != nil {
		t.Fatalf("DeleteByKey replay: %v", err)
	}

	archived, err := s.ListArchivedByKey(ctx, "dangzalo", historyKey, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	// 4 raw + the summary row the clear removed.
	if len(archived) != 5 {
		t.Fatalf("archived %d rows, want 5", len(archived))
	}
	seen := make(map[uuid.UUID]bool, len(archived))
	for _, a := range archived {
		if seen[a.ID] {
			t.Fatalf("archive contains duplicate id %s", a.ID)
		}
		seen[a.ID] = true
	}
}

// Archived rows stay inside their tenant.
func TestPGListArchivedByKeyIsTenantScoped(t *testing.T) {
	historyKey := "group-" + uuid.NewString()
	s, ctx, _ := seedPendingGroup(t, historyKey, 2)
	if err := s.DeleteByKey(ctx, "dangzalo", historyKey); err != nil {
		t.Fatalf("DeleteByKey: %v", err)
	}

	db := testDB(t)
	otherTenant, _ := seedTenantAgent(t, db)
	otherCtx := store.WithTenantID(context.Background(), otherTenant)

	archived, err := s.ListArchivedByKey(otherCtx, "dangzalo", historyKey, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	if len(archived) != 0 {
		t.Fatalf("other tenant sees %d archived rows, want 0", len(archived))
	}
}
