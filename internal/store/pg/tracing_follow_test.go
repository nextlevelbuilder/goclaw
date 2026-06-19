package pg

import (
	"strings"
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

func TestBuildTraceWhereAdvancedFilters(t *testing.T) {
	tenantID := uuid.New()
	from := time.Date(2026, 6, 10, 1, 2, 3, 0, time.UTC)
	to := time.Date(2026, 6, 11, 4, 5, 6, 0, time.UTC)
	minIn, maxIn := 10, 20
	minOut, maxOut := 30, 40
	minTools, maxTools := 1, 3
	hasTools := true

	where, args := buildTraceWhere(store.WithTenantID(t.Context(), tenantID), store.TraceListOpts{
		Query:           `abc_%\`,
		From:            &from,
		To:              &to,
		AgentQuery:      "helper",
		ChannelQuery:    "ops",
		MinInputTokens:  &minIn,
		MaxInputTokens:  &maxIn,
		MinOutputTokens: &minOut,
		MaxOutputTokens: &maxOut,
		MinToolCalls:    &minTools,
		MaxToolCalls:    &maxTools,
		ToolName:        `web_%`,
		HasToolCalls:    &hasTools,
	})

	for _, fragment := range []string{
		"tenant_id = $1",
		"start_time >= $2",
		"start_time < $3",
		"total_input_tokens >= $4",
		"total_input_tokens <= $5",
		"total_output_tokens >= $6",
		"total_output_tokens <= $7",
		"tool_call_count >= $8",
		"tool_call_count <= $9",
		"tool_call_count > 0",
		"CAST(id AS text) ILIKE $10 ESCAPE '\\'",
		"EXISTS (SELECT 1 FROM agents a WHERE a.id = traces.agent_id AND a.tenant_id = traces.tenant_id",
		"EXISTS (SELECT 1 FROM channel_instances ci WHERE ci.name = traces.channel AND ci.tenant_id = traces.tenant_id",
		"EXISTS (SELECT 1 FROM spans s WHERE s.trace_id = traces.id AND s.tenant_id = traces.tenant_id",
		"s.tool_name ILIKE $13 ESCAPE '\\'",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where missing %q:\n%s", fragment, where)
		}
	}
	if len(args) != 13 {
		t.Fatalf("args len = %d, want 13: %#v", len(args), args)
	}
	if args[9] != `%abc\_\%\\%` {
		t.Fatalf("query arg = %#v, want escaped contains pattern", args[9])
	}
	if args[12] != `%web\_\%%` {
		t.Fatalf("tool arg = %#v, want escaped contains pattern", args[12])
	}
}
