package methods

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/clisession"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// TestRouteForSessionKey is the whole early-branch decision. The agent cases
// matter more than the CLI one: if any of them ever routed to the CLI handler,
// every existing conversation would break.
func TestRouteForSessionKey(t *testing.T) {
	cliCases := []string{
		sessions.BuildCLISessionKey(uuid.NewString(), uuid.NewString()),
		"cli:conn:conv",
		"cli:", // malformed but claimed: the CLI handler explains, the agent path never sees it
	}
	for _, key := range cliCases {
		if got := routeForSessionKey(key); got != routeCLI {
			t.Errorf("routeForSessionKey(%q) = %v, want routeCLI", key, got)
		}
	}

	agentCases := []string{
		"", // no session key at all — the agent path mints a ws: key
		sessions.BuildWSSessionKey("default", uuid.NewString()),
		sessions.BuildSessionKey("default", "telegram", sessions.PeerDirect, "42"),
		sessions.BuildSubagentSessionKey("default", "task"),
		sessions.BuildCronSessionKey("default", "job"),
		sessions.BuildTeamSessionKey("default", "team", "chat"),
		// An agent whose KEY is "cli" is still an agent session.
		sessions.BuildWSSessionKey("cli", uuid.NewString()),
		"agent:cli:ws:direct:x",
		// Near-misses that must not trip the prefix test.
		"clip:conn:conv",
		"CLI:conn:conv",
		" cli:conn:conv",
	}
	for _, key := range agentCases {
		if got := routeForSessionKey(key); got != routeAgent {
			t.Errorf("routeForSessionKey(%q) = %v, want routeAgent", key, got)
		}
	}
}

// ---------------------------------------------------------------------------
// PermissionFunc → approval broker
// ---------------------------------------------------------------------------

// newTestRelay builds a relay wired to a real approval broker, with no session,
// sandbox or store behind it — the permission bridge needs none of those.
func newTestRelay(broker *tools.ExecApprovalManager, mode cliagent.PermissionMode, timeout time.Duration) *cliRelay {
	chat := NewCLIChat(CLIChatDeps{Approvals: broker, ApprovalTimeout: timeout})
	connID := uuid.New()
	r := &cliRelay{chat: chat, sessionKey: sessions.BuildCLISessionKey(connID.String(), "conv-1")}
	r.configure(&store.CLIConnection{ID: connID, Name: "Claude Code"}, mode, "user-1", uuid.New())
	return r
}

func bashRequest() clisession.PermissionRequest {
	return clisession.PermissionRequest{
		RequestID:   "req-1",
		ToolName:    "Bash",
		DisplayName: "Bash",
		Input:       json.RawMessage(`{"command":"git push origin main"}`),
	}
}

// waitForPending blocks until the broker has exactly one pending row, returning
// its id. Polling (rather than a sleep) keeps the test fast and non-flaky.
func waitForPending(t *testing.T, broker *tools.ExecApprovalManager) *tools.PendingApproval {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if list := broker.ListPending(); len(list) == 1 {
			return list[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no approval was raised within the deadline")
	return nil
}

func TestPermissionManualAllow(t *testing.T) {
	broker := tools.NewExecApprovalManager(tools.DefaultExecApprovalConfig())
	relay := newTestRelay(broker, cliagent.PermissionManual, time.Minute)

	decisions := make(chan clisession.PermissionDecision, 1)
	go func() { decisions <- relay.permission(context.Background(), bashRequest()) }()

	pending := waitForPending(t, broker)
	// The prompt must carry what the CLI wants to do, scoped to the asking user.
	if !strings.Contains(pending.Detail, "git push origin main") {
		t.Errorf("approval detail = %q, want it to name the command", pending.Detail)
	}
	if !strings.Contains(pending.Detail, "Claude Code") {
		t.Errorf("approval detail = %q, want it to name the connection", pending.Detail)
	}
	if pending.ToolName != "cli:Bash" {
		t.Errorf("approval toolName = %q, want %q", pending.ToolName, "cli:Bash")
	}
	if pending.UserID != "user-1" {
		t.Errorf("approval userId = %q, want user-1 (the event filter scopes on it)", pending.UserID)
	}
	if pending.SessionKey != relay.sessionKey {
		t.Errorf("approval sessionKey = %q, want %q", pending.SessionKey, relay.sessionKey)
	}
	// Command must stay EMPTY: a non-empty one would let "allow always" widen the
	// agent's own exec allowlist. See the comment in cliRelay.permission.
	if pending.Command != "" {
		t.Errorf("approval command = %q, want empty", pending.Command)
	}

	if err := broker.Resolve(pending.ID, tools.ApprovalAllowOnce); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	decision := <-decisions
	if !decision.Allow {
		t.Fatalf("decision = deny(%q), want allow", decision.DenyReason)
	}
	if len(decision.UpdatedInput) != 0 {
		t.Errorf("UpdatedInput = %q, want none (we do not rewrite the tool input)", decision.UpdatedInput)
	}
}

func TestPermissionManualDenyCarriesReason(t *testing.T) {
	broker := tools.NewExecApprovalManager(tools.DefaultExecApprovalConfig())
	relay := newTestRelay(broker, cliagent.PermissionManual, time.Minute)

	decisions := make(chan clisession.PermissionDecision, 1)
	go func() { decisions <- relay.permission(context.Background(), bashRequest()) }()

	pending := waitForPending(t, broker)
	if err := broker.Resolve(pending.ID, tools.ApprovalDeny); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	decision := <-decisions
	if decision.Allow {
		t.Fatal("decision = allow, want deny")
	}
	// The reason is shown to the model as the tool's error result — an empty one
	// leaves it guessing why it was blocked.
	if strings.TrimSpace(decision.DenyReason) == "" {
		t.Fatal("DenyReason is empty on a user denial")
	}
	if !strings.Contains(strings.ToLower(decision.DenyReason), "declined") {
		t.Errorf("DenyReason = %q, want it to say the user declined", decision.DenyReason)
	}
}

// TestPermissionTimeoutDenies is the security-critical one: nobody answered, so
// nobody consented, and the action must be refused rather than assumed.
func TestPermissionTimeoutDenies(t *testing.T) {
	broker := tools.NewExecApprovalManager(tools.DefaultExecApprovalConfig())
	relay := newTestRelay(broker, cliagent.PermissionManual, 40*time.Millisecond)

	start := time.Now()
	decision := relay.permission(context.Background(), bashRequest())
	if decision.Allow {
		t.Fatal("an unanswered approval was ALLOWED — a timeout must deny")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %s, want ~40ms", elapsed)
	}
	if !strings.Contains(decision.DenyReason, "NOT run") {
		t.Errorf("DenyReason = %q, want it to say the action was not run", decision.DenyReason)
	}
	if !strings.Contains(strings.ToLower(decision.DenyReason), "approve") {
		t.Errorf("DenyReason = %q, want it to explain that nobody approved", decision.DenyReason)
	}
	// The broker must not leak the pending row after a timeout.
	if list := broker.ListPending(); len(list) != 0 {
		t.Errorf("pending approvals after timeout = %d, want 0", len(list))
	}
}

// TestPermissionSessionEndedDenies covers the shutdown path: Session.Close waits
// for pending approvals, so a cancelled session context must unblock the wait
// immediately — and refuse, because nobody answered.
func TestPermissionSessionEndedDenies(t *testing.T) {
	broker := tools.NewExecApprovalManager(tools.DefaultExecApprovalConfig())
	relay := newTestRelay(broker, cliagent.PermissionManual, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	decisions := make(chan clisession.PermissionDecision, 1)
	go func() { decisions <- relay.permission(ctx, bashRequest()) }()

	waitForPending(t, broker)
	cancel()

	select {
	case decision := <-decisions:
		if decision.Allow {
			t.Fatal("decision = allow after the session ended, want deny")
		}
		if !strings.Contains(strings.ToLower(decision.DenyReason), "ended") {
			t.Errorf("DenyReason = %q, want it to say the conversation ended", decision.DenyReason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("permission did not return after the session context was cancelled")
	}
}

// TestPermissionAutoModeRaisesNoApproval pins the mode contract: in auto mode the
// CLI runs with bypassPermissions and the user has already consented, so no
// prompt may be raised.
func TestPermissionAutoModeRaisesNoApproval(t *testing.T) {
	broker := tools.NewExecApprovalManager(tools.DefaultExecApprovalConfig())
	relay := newTestRelay(broker, cliagent.PermissionAuto, time.Minute)

	decision := relay.permission(context.Background(), bashRequest())
	if !decision.Allow {
		t.Fatalf("auto mode denied (%q), want allow without prompting", decision.DenyReason)
	}
	if list := broker.ListPending(); len(list) != 0 {
		t.Fatalf("auto mode raised %d approval(s), want 0", len(list))
	}
}

// TestPermissionManualModeRaisesOne is the counterpart: manual mode must put
// every action in front of a human.
func TestPermissionManualModeRaisesOne(t *testing.T) {
	broker := tools.NewExecApprovalManager(tools.DefaultExecApprovalConfig())
	relay := newTestRelay(broker, cliagent.PermissionManual, time.Minute)

	go relay.permission(context.Background(), bashRequest())

	pending := waitForPending(t, broker)
	_ = broker.Resolve(pending.ID, tools.ApprovalDeny) // release the goroutine
}

// TestPermissionNoBrokerDenies: a manual-mode conversation with no approval
// channel must refuse, never fall through to allow. Send() refuses such a
// conversation up front, so this guards the defence-in-depth branch.
func TestPermissionNoBrokerDenies(t *testing.T) {
	relay := newTestRelay(nil, cliagent.PermissionManual, time.Minute)
	decision := relay.permission(context.Background(), bashRequest())
	if decision.Allow {
		t.Fatal("no approval channel was treated as consent")
	}
	if strings.TrimSpace(decision.DenyReason) == "" {
		t.Fatal("DenyReason is empty")
	}
}

// ---------------------------------------------------------------------------
// Approval detail rendering
// ---------------------------------------------------------------------------

func TestCLIApprovalDetail(t *testing.T) {
	cases := []struct {
		name string
		req  clisession.PermissionRequest
		want []string
	}{
		{
			name: "prefers the CLI's own description",
			req: clisession.PermissionRequest{
				ToolName: "WebFetch", DisplayName: "WebFetch",
				Description: "Fetch example.com",
				Input:       json.RawMessage(`{"url":"https://example.com"}`),
			},
			want: []string{"WebFetch", "Fetch example.com"},
		},
		{
			name: "falls back to the consequential input field",
			req: clisession.PermissionRequest{
				ToolName: "Bash",
				Input:    json.RawMessage(`{"description":"x","command":"rm -rf build"}`),
			},
			want: []string{"Bash", "rm -rf build"},
		},
		{
			name: "file edits name the path",
			req: clisession.PermissionRequest{
				ToolName: "Edit",
				Input:    json.RawMessage(`{"file_path":"/app/main.go","old_string":"a"}`),
			},
			want: []string{"Edit", "/app/main.go"},
		},
		{
			name: "unknown shape still says something",
			req: clisession.PermissionRequest{
				ToolName: "Mystery",
				Input:    json.RawMessage(`{"weird":"value"}`),
			},
			want: []string{"Mystery", "weird"},
		},
		{
			name: "no input at all",
			req:  clisession.PermissionRequest{ToolName: "Bash"},
			want: []string{"Bash"},
		},
	}
	for _, tc := range cases {
		got := cliApprovalDetail("Claude Code", tc.req)
		if !strings.Contains(got, "Claude Code") {
			t.Errorf("%s: detail %q does not name the connection", tc.name, got)
		}
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: detail %q missing %q", tc.name, got, want)
			}
		}
	}
}

func TestCLITruncateStaysValidUTF8(t *testing.T) {
	// Cutting mid-rune would emit invalid UTF-8, which the JSON encoder mangles.
	s := strings.Repeat("é", 10) // 20 bytes
	got := cliTruncate(s, 5)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("cliTruncate(%q, 5) = %q, want an ellipsis marker", s, got)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("cliTruncate produced invalid UTF-8: %q", got)
		}
	}
	if cliTruncate("short", 100) != "short" {
		t.Error("cliTruncate shortened a string that fits")
	}
}

// ---------------------------------------------------------------------------
// Turn accumulation
// ---------------------------------------------------------------------------

// TestCLITurnAccumulates pins what a reload will show: the terminal result wins
// as the reply, narration is kept as the fallback, and each tool call carries the
// name its result event omits.
func TestCLITurnAccumulates(t *testing.T) {
	relay := newTestRelay(nil, cliagent.PermissionAuto, time.Minute)
	turn := relay.beginTurn("run-1")

	relay.onEvent(cliagent.Event{Kind: cliagent.EventText, Text: "Looking at "})
	relay.onEvent(cliagent.Event{Kind: cliagent.EventText, Text: "the repo.\n"})
	relay.onEvent(cliagent.Event{Kind: cliagent.EventToolCall, ToolID: "t1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)})
	relay.onEvent(cliagent.Event{Kind: cliagent.EventToolResult, ToolID: "t1", ToolResult: "main.go"})
	relay.onEvent(cliagent.Event{Kind: cliagent.EventFinal, Text: "Done."})

	select {
	case <-turn.done:
	case <-time.After(time.Second):
		t.Fatal("the terminal event did not finish the turn")
	}

	final, isErr, records := turn.snapshot()
	if final != "Done." {
		t.Errorf("final = %q, want %q", final, "Done.")
	}
	if isErr {
		t.Error("isError = true, want false")
	}
	if got := turn.narration(); got != "Looking at the repo." {
		t.Errorf("narration = %q, want the streamed text", got)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Name != "Bash" || records[0].Result != "main.go" || !records[0].Answered {
		t.Errorf("record = %+v, want an answered Bash call", records[0])
	}
}

// TestCLITurnDropsEventsAfterEnd: once a turn is over, late narration from the
// CLI must not be attributed to whatever turn comes next.
func TestCLITurnDropsEventsAfterEnd(t *testing.T) {
	relay := newTestRelay(nil, cliagent.PermissionAuto, time.Minute)
	first := relay.beginTurn("run-1")
	relay.endTurn(first)

	relay.onEvent(cliagent.Event{Kind: cliagent.EventText, Text: "late"})

	second := relay.beginTurn("run-2")
	if got := second.narration(); got != "" {
		t.Errorf("second turn picked up %q from the previous turn", got)
	}
	if got := first.narration(); got != "" {
		t.Errorf("ended turn accumulated %q after endTurn", got)
	}
}

// TestCLIChatReady: the layer must refuse honestly when a half it needs is
// missing, instead of handing back a session key that could never work.
func TestCLIChatReady(t *testing.T) {
	if (*CLIChat)(nil).Ready() {
		t.Error("nil CLIChat reported ready")
	}
	if NewCLIChat(CLIChatDeps{}).Ready() {
		t.Error("CLIChat with no manager/store/sandbox reported ready")
	}
	full := NewCLIChat(CLIChatDeps{
		Manager:     clisession.NewManager(clisession.ManagerOpts{}),
		Connections: &stubCLIConnStore{},
		Sandbox:     stubSandboxManager{},
	})
	defer full.Shutdown()
	if !full.Ready() {
		t.Error("fully wired CLIChat reported not ready")
	}
}

// TestInteractiveSandboxKeyStaysWithinBudget guards the silent truncation in
// sandbox.sanitizeKey: a key over 50 chars is CUT, which would collapse two
// users' containers into one.
func TestInteractiveSandboxKeyStaysWithinBudget(t *testing.T) {
	ctx := store.WithUserID(store.WithTenantID(context.Background(), uuid.New()), "a-very-long-cognito-subject-identifier-000000")
	key := tools.InteractiveSandboxKey(ctx, uuid.NewString())
	if len(key) > 50 {
		t.Fatalf("InteractiveSandboxKey = %q (%d chars), want <= 50", key, len(key))
	}
	if !strings.HasPrefix(key, "cli:") {
		t.Errorf("InteractiveSandboxKey = %q, want a cli: prefix so it never shares the delegation container", key)
	}

	// Different users must not share a container: the credential lives in the
	// container's HOME between turns.
	other := store.WithUserID(store.WithTenantID(context.Background(), uuid.New()), "someone-else")
	if tools.InteractiveSandboxKey(other, "conn-1") == tools.InteractiveSandboxKey(ctx, "conn-1") {
		t.Error("two different users produced the same sandbox key")
	}
}
