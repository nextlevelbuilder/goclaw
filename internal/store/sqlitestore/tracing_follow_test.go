//go:build sqlite || sqliteonly

package sqlitestore

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestBuildTraceWhereChangedAfterKeepsExistingFiltersGrouped(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	since := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)

	where, args := buildTraceWhere(store.WithTenantID(t.Context(), tenantID), store.TraceListOpts{
		AgentID:      &agentID,
		UserID:       "caller",
		SessionKey:   "session-1",
		Status:       store.TraceStatusError,
		Channel:      "telegram",
		ChangedAfter: &since,
	})

	want := " WHERE tenant_id = ? AND agent_id = ? AND user_id = ? AND session_key = ? AND status = ? AND channel = ? AND (created_at > ? OR end_time > ? OR status = ?)"
	if where != want {
		t.Fatalf("where = %q, want %q", where, want)
	}
	if len(args) != 9 {
		t.Fatalf("args len = %d, want 9: %#v", len(args), args)
	}
	if args[6] != since || args[7] != since {
		t.Fatalf("changed-after args = %#v/%#v, want %#v", args[6], args[7], since)
	}
	if args[8] != store.TraceStatusRunning {
		t.Fatalf("running status arg = %#v, want %q", args[8], store.TraceStatusRunning)
	}
}
