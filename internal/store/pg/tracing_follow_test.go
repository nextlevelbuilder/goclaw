package pg

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

	want := " WHERE tenant_id = $1 AND agent_id = $2 AND user_id = $3 AND session_key = $4 AND status = $5 AND channel = $6 AND (created_at > $7 OR end_time > $7 OR status = $8)"
	if where != want {
		t.Fatalf("where = %q, want %q", where, want)
	}
	if len(args) != 8 {
		t.Fatalf("args len = %d, want 8: %#v", len(args), args)
	}
	if args[6] != since {
		t.Fatalf("changed-after arg = %#v, want %#v", args[6], since)
	}
	if args[7] != store.TraceStatusRunning {
		t.Fatalf("running status arg = %#v, want %q", args[7], store.TraceStatusRunning)
	}
}
