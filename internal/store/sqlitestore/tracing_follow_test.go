//go:build sqlite || sqliteonly

package sqlitestore

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

func TestBuildTraceWhereAdvancedFilters(t *testing.T) {
	tenantID := uuid.New()
	from := time.Date(2026, 6, 10, 1, 2, 3, 0, time.UTC)
	to := time.Date(2026, 6, 11, 4, 5, 6, 0, time.UTC)
	minIn, maxIn := 10, 20
	minOut, maxOut := 30, 40
	minTools, maxTools := 1, 3
	hasTools := false

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
		"tenant_id = ?",
		"start_time >= ?",
		"start_time < ?",
		"total_input_tokens >= ?",
		"total_input_tokens <= ?",
		"total_output_tokens >= ?",
		"total_output_tokens <= ?",
		"tool_call_count >= ?",
		"tool_call_count <= ?",
		"tool_call_count = 0",
		"CAST(id AS TEXT) LIKE ? ESCAPE '\\'",
		"EXISTS (SELECT 1 FROM agents a WHERE a.id = traces.agent_id AND a.tenant_id = traces.tenant_id",
		"EXISTS (SELECT 1 FROM channel_instances ci WHERE ci.name = traces.channel AND ci.tenant_id = traces.tenant_id",
		"EXISTS (SELECT 1 FROM spans s WHERE s.trace_id = traces.id AND s.tenant_id = traces.tenant_id",
		"s.tool_name LIKE ? ESCAPE '\\'",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where missing %q:\n%s", fragment, where)
		}
	}
	if len(args) != 29 {
		t.Fatalf("args len = %d, want 29: %#v", len(args), args)
	}
	if args[9] != `%abc\_\%\\%` {
		t.Fatalf("query arg = %#v, want escaped contains pattern", args[9])
	}
	if args[28] != `%web\_\%%` {
		t.Fatalf("tool arg = %#v, want escaped contains pattern", args[28])
	}
}
