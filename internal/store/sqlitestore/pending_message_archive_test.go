//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// newPendingFixture returns a store plus a tenant-scoped context with n group
// messages already buffered, oldest first.
func newPendingFixture(t *testing.T, slug string, n int) (*SQLitePendingMessageStore, context.Context, *sql.DB, []store.PendingMessage) {
	t.Helper()
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', ?, 'active')`, tenantID.String(), slug); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), tenantID)

	base := time.Now().Add(-time.Duration(n) * time.Minute).UTC().Truncate(time.Second)
	msgs := make([]store.PendingMessage, n)
	for i := range msgs {
		msgs[i] = store.PendingMessage{
			ID:            uuid.Must(uuid.NewV7()),
			ChannelName:   "zalo-main",
			HistoryKey:    "group-1",
			Sender:        "Diệu Hương",
			SenderID:      "uid-1",
			Body:          "raw message " + uuid.NewString(),
			PlatformMsgID: uuid.NewString(),
			CreatedAt:     base.Add(time.Duration(i) * time.Minute),
		}
	}
	pending := NewSQLitePendingMessageStore(db)
	if err := pending.AppendBatch(ctx, msgs); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	return pending, ctx, db, msgs
}

func archivedBodies(t *testing.T, archived []store.ArchivedMessage) map[string]string {
	t.Helper()
	out := make(map[string]string, len(archived))
	for _, a := range archived {
		out[a.Body] = a.ArchiveReason
	}
	return out
}

// Clearing the buffer after the bot is mentioned must not destroy the raw
// messages: they are the only stored copy of the group conversation.
func TestDeleteByKeyArchivesMessages(t *testing.T) {
	pending, ctx, _, msgs := newPendingFixture(t, "t-archive-clear", 3)

	if err := pending.DeleteByKey(ctx, "zalo-main", "group-1"); err != nil {
		t.Fatalf("DeleteByKey: %v", err)
	}

	live, err := pending.ListByKey(ctx, "zalo-main", "group-1")
	if err != nil {
		t.Fatalf("ListByKey: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("buffer has %d rows after clear, want 0", len(live))
	}

	archived, err := pending.ListArchivedByKey(ctx, "zalo-main", "group-1", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	if len(archived) != len(msgs) {
		t.Fatalf("archived %d rows, want %d", len(archived), len(msgs))
	}
	got := archivedBodies(t, archived)
	for _, m := range msgs {
		reason, ok := got[m.Body]
		if !ok {
			t.Fatalf("message %q missing from archive", m.Body)
		}
		if reason != store.ArchiveReasonConsumed {
			t.Fatalf("archive reason = %q, want %q", reason, store.ArchiveReasonConsumed)
		}
	}
	// Chronological order is what a replay depends on.
	for i := 1; i < len(archived); i++ {
		if archived[i].CreatedAt.Before(archived[i-1].CreatedAt) {
			t.Fatalf("archive not ordered by created_at at index %d", i)
		}
	}
}

// Compaction replaces old messages with an LLM summary. The summary is lossy,
// so the raw rows it replaces must survive in the archive.
func TestCompactArchivesReplacedMessages(t *testing.T) {
	pending, ctx, _, msgs := newPendingFixture(t, "t-archive-compact", 5)

	deleteIDs := []uuid.UUID{msgs[0].ID, msgs[1].ID, msgs[2].ID}
	summary := &store.PendingMessage{
		ChannelName: "zalo-main",
		HistoryKey:  "group-1",
		Sender:      "[summary]",
		Body:        "Concise summary of the first three messages.",
		IsSummary:   true,
	}
	if err := pending.Compact(ctx, deleteIDs, summary); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	live, err := pending.ListByKey(ctx, "zalo-main", "group-1")
	if err != nil {
		t.Fatalf("ListByKey: %v", err)
	}
	if len(live) != 3 { // 2 kept raw + 1 summary
		t.Fatalf("buffer has %d rows after compaction, want 3", len(live))
	}

	archived, err := pending.ListArchivedByKey(ctx, "zalo-main", "group-1", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	if len(archived) != 3 {
		t.Fatalf("archived %d rows, want 3", len(archived))
	}
	got := archivedBodies(t, archived)
	for _, m := range msgs[:3] {
		reason, ok := got[m.Body]
		if !ok {
			t.Fatalf("compacted message %q missing from archive", m.Body)
		}
		if reason != store.ArchiveReasonCompacted {
			t.Fatalf("archive reason = %q, want %q", reason, store.ArchiveReasonCompacted)
		}
	}
	for _, m := range msgs[3:] {
		if _, ok := got[m.Body]; ok {
			t.Fatalf("kept message %q must not be archived yet", m.Body)
		}
	}
}

// A group that is compacted and later cleared must end up with every raw
// message archived exactly once — archived rows keep their original id.
func TestArchiveIsIdempotentAcrossCompactThenClear(t *testing.T) {
	pending, ctx, db, msgs := newPendingFixture(t, "t-archive-idempotent", 4)

	summary := &store.PendingMessage{
		ChannelName: "zalo-main",
		HistoryKey:  "group-1",
		Sender:      "[summary]",
		Body:        "Summary.",
		IsSummary:   true,
	}
	if err := pending.Compact(ctx, []uuid.UUID{msgs[0].ID, msgs[1].ID}, summary); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := pending.DeleteByKey(ctx, "zalo-main", "group-1"); err != nil {
		t.Fatalf("DeleteByKey: %v", err)
	}
	// Replaying the clear must not duplicate rows.
	if err := pending.DeleteByKey(ctx, "zalo-main", "group-1"); err != nil {
		t.Fatalf("DeleteByKey replay: %v", err)
	}

	archived, err := pending.ListArchivedByKey(ctx, "zalo-main", "group-1", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	// 4 raw + the summary row the clear removed.
	if len(archived) != 5 {
		t.Fatalf("archived %d rows, want 5", len(archived))
	}
	got := archivedBodies(t, archived)
	for _, m := range msgs {
		if _, ok := got[m.Body]; !ok {
			t.Fatalf("message %q missing from archive", m.Body)
		}
	}

	var distinct int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT id) FROM channel_message_archive`).Scan(&distinct); err != nil {
		t.Fatalf("count distinct: %v", err)
	}
	if distinct != len(archived) {
		t.Fatalf("archive has %d rows but %d distinct ids", len(archived), distinct)
	}
}

// The archive is tenant-scoped like every other read.
func TestListArchivedByKeyIsTenantScoped(t *testing.T) {
	pending, ctx, db, _ := newPendingFixture(t, "t-archive-tenant", 2)
	if err := pending.DeleteByKey(ctx, "zalo-main", "group-1"); err != nil {
		t.Fatalf("DeleteByKey: %v", err)
	}

	otherTenant := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T2', 't-archive-tenant-2', 'active')`, otherTenant.String()); err != nil {
		t.Fatalf("insert other tenant: %v", err)
	}
	otherCtx := store.WithTenantID(context.Background(), otherTenant)

	archived, err := pending.ListArchivedByKey(otherCtx, "zalo-main", "group-1", time.Time{}, 0)
	if err != nil {
		t.Fatalf("ListArchivedByKey: %v", err)
	}
	if len(archived) != 0 {
		t.Fatalf("other tenant sees %d archived rows, want 0", len(archived))
	}
}
