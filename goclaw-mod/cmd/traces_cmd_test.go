package cmd

import (
	"net/url"
	"testing"
)

func TestBuildTraceListPathIncludesFilters(t *testing.T) {
	path := buildTraceListPath(traceListOptions{
		Query:        "provider fail",
		SessionKey:   "session A",
		Status:       "running",
		AgentQuery:   "coder",
		HasToolCalls: "true",
		Limit:        25,
		Offset:       10,
	})

	u, err := url.Parse(path)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	if u.Path != "/v1/traces" {
		t.Fatalf("path = %q, want /v1/traces", u.Path)
	}
	q := u.Query()
	assertQueryValue(t, q, "q", "provider fail")
	assertQueryValue(t, q, "session_key", "session A")
	assertQueryValue(t, q, "status", "running")
	assertQueryValue(t, q, "agent", "coder")
	assertQueryValue(t, q, "has_tool_calls", "true")
	assertQueryValue(t, q, "limit", "25")
	assertQueryValue(t, q, "offset", "10")
}

func TestBuildTraceFollowPathRequiresScope(t *testing.T) {
	if _, err := buildTraceFollowPath(traceFollowOptions{}); err == nil {
		t.Fatal("expected missing scope error")
	}

	path, err := buildTraceFollowPath(traceFollowOptions{
		SessionKey:   "session A",
		Since:        "2026-06-12T01:00:00Z",
		Limit:        20,
		IncludeSpans: true,
	})
	if err != nil {
		t.Fatalf("buildTraceFollowPath: %v", err)
	}
	u, err := url.Parse(path)
	if err != nil {
		t.Fatalf("parse path: %v", err)
	}
	if u.Path != "/v1/traces/follow" {
		t.Fatalf("path = %q, want /v1/traces/follow", u.Path)
	}
	q := u.Query()
	assertQueryValue(t, q, "session_key", "session A")
	assertQueryValue(t, q, "since", "2026-06-12T01:00:00Z")
	assertQueryValue(t, q, "limit", "20")
	assertQueryValue(t, q, "include_spans", "true")
}

func TestTraceTimelinePathUsesRunIDFromDetail(t *testing.T) {
	runID, err := traceRunIDFromDetail(traceDetailResponse{
		Trace: traceDataForCLI{RunID: "run-123"},
	})
	if err != nil {
		t.Fatalf("traceRunIDFromDetail: %v", err)
	}
	if runID != "run-123" {
		t.Fatalf("runID = %q, want run-123", runID)
	}

	if _, err := traceRunIDFromDetail(traceDetailResponse{}); err == nil {
		t.Fatal("expected missing run_id error")
	}
}

func TestValidateTraceOutputFormatRejectsUnsupportedValues(t *testing.T) {
	oldOutput := gatewayOutputFormat
	t.Cleanup(func() { gatewayOutputFormat = oldOutput })

	gatewayOutputFormat = "yaml"
	if err := validateTraceOutputFormat(); err == nil {
		t.Fatal("expected unsupported output format error")
	}
}

func assertQueryValue(t *testing.T, q url.Values, key, want string) {
	t.Helper()
	if got := q.Get(key); got != want {
		t.Fatalf("query %s = %q, want %q", key, got, want)
	}
}
