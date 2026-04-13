package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ─── uniquifyToolCallIDs ──────────────────────────────────────────────────

func TestUniquifyToolCallIDs_Empty(t *testing.T) {
	got := uniquifyToolCallIDs(nil, "run-1", 1)
	if got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}

func TestUniquifyToolCallIDs_SingleCall(t *testing.T) {
	calls := []providers.ToolCall{{ID: "orig-1", Name: "tool"}}
	got := uniquifyToolCallIDs(calls, "run-abc", 1)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	// ID should be rewritten.
	if got[0].ID == "orig-1" {
		t.Error("ID should have been rewritten")
	}
	// Must start with "call_" and be 40 chars.
	if !strings.HasPrefix(got[0].ID, "call_") {
		t.Errorf("ID should start with 'call_', got %q", got[0].ID)
	}
	if len(got[0].ID) != 40 {
		t.Errorf("ID length = %d, want 40", len(got[0].ID))
	}
}

func TestUniquifyToolCallIDs_Deterministic(t *testing.T) {
	calls := []providers.ToolCall{{ID: "orig-1", Name: "tool"}}
	got1 := uniquifyToolCallIDs(calls, "run-abc", 1)
	got2 := uniquifyToolCallIDs(calls, "run-abc", 1)
	if got1[0].ID != got2[0].ID {
		t.Errorf("same inputs should produce same ID: %q vs %q", got1[0].ID, got2[0].ID)
	}
}

func TestUniquifyToolCallIDs_DifferentRunIDsDifferentOutput(t *testing.T) {
	calls := []providers.ToolCall{{ID: "orig-1", Name: "tool"}}
	got1 := uniquifyToolCallIDs(calls, "run-aaa", 1)
	got2 := uniquifyToolCallIDs(calls, "run-bbb", 1)
	if got1[0].ID == got2[0].ID {
		t.Error("different run IDs should produce different call IDs")
	}
}

func TestUniquifyToolCallIDs_DifferentIterationsDifferentOutput(t *testing.T) {
	calls := []providers.ToolCall{{ID: "orig-1", Name: "tool"}}
	got1 := uniquifyToolCallIDs(calls, "run-same", 1)
	got2 := uniquifyToolCallIDs(calls, "run-same", 2)
	if got1[0].ID == got2[0].ID {
		t.Error("different iterations should produce different call IDs")
	}
}

func TestUniquifyToolCallIDs_MultipleCalls_UniqueIDs(t *testing.T) {
	calls := []providers.ToolCall{
		{ID: "dup-id", Name: "tool1"},
		{ID: "dup-id", Name: "tool2"}, // same original ID
	}
	got := uniquifyToolCallIDs(calls, "run-x", 1)
	if got[0].ID == got[1].ID {
		t.Errorf("duplicate original IDs should produce unique outputs: both = %q", got[0].ID)
	}
}

func TestUniquifyToolCallIDs_DoesNotMutateInput(t *testing.T) {
	orig := "original-id"
	calls := []providers.ToolCall{{ID: orig, Name: "tool"}}
	_ = uniquifyToolCallIDs(calls, "run-x", 1)
	if calls[0].ID != orig {
		t.Error("input slice should not be mutated")
	}
}

// ─── shouldShareKnowledgeGraph ───────────────────────────────────────────

func TestShouldShareKnowledgeGraph_NilConfig(t *testing.T) {
	l := &Loop{workspaceSharing: nil}
	if l.shouldShareKnowledgeGraph() {
		t.Error("nil config should return false")
	}
}

func TestShouldShareKnowledgeGraph_EnabledConfig(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareKnowledgeGraph: true}}
	if !l.shouldShareKnowledgeGraph() {
		t.Error("ShareKnowledgeGraph=true should return true")
	}
}

func TestShouldShareKnowledgeGraph_DisabledByDefault(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: true}}
	if l.shouldShareKnowledgeGraph() {
		t.Error("ShareMemory alone should not enable KG sharing")
	}
}

// ─── shouldShareSessions ──────────────────────────────────────────────────────

func TestShouldShareSessions_NilConfig(t *testing.T) {
	l := &Loop{workspaceSharing: nil}
	if l.shouldShareSessions() {
		t.Error("nil config should return false")
	}
}

func TestShouldShareSessions_EnabledConfig(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareSessions: true}}
	if !l.shouldShareSessions() {
		t.Error("ShareSessions=true should return true")
	}
}

func TestShouldShareSessions_DisabledByDefault(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: true, ShareKnowledgeGraph: true}}
	if l.shouldShareSessions() {
		t.Error("ShareMemory and ShareKnowledgeGraph alone should not enable sessions sharing")
	}
}

func TestShouldShareSessions_IndependentOfMemory(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		ShareMemory:    true,
		ShareSessions: false,
	}}
	if l.shouldShareSessions() {
		t.Error("ShareMemory=true with ShareSessions=false should return false (independent)")
	}
}

// ─── InvalidateUserWorkspace ──────────────────────────────────────────────

func TestInvalidateUserWorkspace_RemovesCachedSetup(t *testing.T) {
	l := &Loop{}
	userID := "user-abc"
	// Store a value.
	l.userSetups.Store(userID, &userSetup{workspace: "/tmp/ws"})

	l.InvalidateUserWorkspace(userID)

	if _, ok := l.userSetups.Load(userID); ok {
		t.Error("userSetup should be removed after invalidate")
	}
}

func TestInvalidateUserWorkspace_NonExistentKeyIsNoop(t *testing.T) {
	l := &Loop{}
	l.InvalidateUserWorkspace("ghost-user") // must not panic
}

// ─── ProviderName ─────────────────────────────────────────────────────────

func TestProviderName_NilProvider(t *testing.T) {
	l := &Loop{}
	if got := l.ProviderName(); got != "" {
		t.Errorf("nil provider should return empty, got %q", got)
	}
}

func TestProviderName_WithProvider(t *testing.T) {
	l := &Loop{provider: &stubProvider{}}
	if got := l.ProviderName(); got != "stub" {
		t.Errorf("ProviderName = %q, want 'stub'", got)
	}
}

// ─── expandWorkspace ──────────────────────────────────────────────────────

func TestExpandWorkspace_AbsolutePathUnchanged(t *testing.T) {
	got := expandWorkspace("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("expandWorkspace = %q, want /absolute/path", got)
	}
}

func TestExpandWorkspace_HomeExpanded(t *testing.T) {
	got := expandWorkspace("~/projects")
	if strings.HasPrefix(got, "~") {
		t.Errorf("tilde not expanded: %q", got)
	}
	if !strings.HasPrefix(got, "/") {
		t.Errorf("expected absolute path after ~ expansion, got %q", got)
	}
}

func TestExpandWorkspace_RelativePathBecomesAbsolute(t *testing.T) {
	got := expandWorkspace("relative/path")
	if !strings.HasPrefix(got, "/") {
		t.Errorf("relative path should become absolute, got %q", got)
	}
}

// ─── buildChannelMeta ─────────────────────────────────────────────────────

func TestBuildChannelMeta_NilRequest(t *testing.T) {
	l := &Loop{}
	if got := l.buildChannelMeta(nil); got != nil {
		t.Errorf("nil request should return nil, got %+v", got)
	}
}

func TestBuildChannelMeta_EmptyChannelType(t *testing.T) {
	l := &Loop{}
	req := &RunRequest{ChannelType: ""}
	if got := l.buildChannelMeta(req); got != nil {
		t.Errorf("empty ChannelType should return nil, got %+v", got)
	}
}

func TestBuildChannelMeta_WithChannelType(t *testing.T) {
	l := &Loop{defaultTimezone: "Asia/Ho_Chi_Minh"}
	req := &RunRequest{
		ChannelType: "telegram",
		SenderName:  "Alice",
	}
	meta := l.buildChannelMeta(req)
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if meta.ChannelType != "telegram" {
		t.Errorf("ChannelType = %q, want 'telegram'", meta.ChannelType)
	}
	if meta.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q, want 'Alice'", meta.DisplayName)
	}
	if meta.DefaultTimezone != "Asia/Ho_Chi_Minh" {
		t.Errorf("DefaultTimezone = %q", meta.DefaultTimezone)
	}
}

// ─── agentToolPolicyWithMCP ───────────────────────────────────────────────

func TestAgentToolPolicyWithMCP_NilPolicyNoMCP(t *testing.T) {
	got := agentToolPolicyWithMCP(nil, false)
	if got != nil {
		t.Errorf("no MCP with nil policy should return nil, got %+v", got)
	}
}

func TestAgentToolPolicyWithMCP_NilPolicyWithMCP(t *testing.T) {
	got := agentToolPolicyWithMCP(nil, true)
	if got == nil {
		t.Fatal("nil policy with MCP should return non-nil policy")
	}
	found := false
	for _, a := range got.AlsoAllow {
		if a == "group:mcp" {
			found = true
		}
	}
	if !found {
		t.Error("expected group:mcp in AlsoAllow")
	}
}

func TestAgentToolPolicyWithMCP_NoDuplicateMCPGroup(t *testing.T) {
	p := &config.ToolPolicySpec{AlsoAllow: []string{"group:mcp", "other"}}
	got := agentToolPolicyWithMCP(p, true)
	count := 0
	for _, a := range got.AlsoAllow {
		if a == "group:mcp" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("group:mcp should appear exactly once, got %d", count)
	}
}

// ─── agentToolPolicyWithWorkspace ─────────────────────────────────────────

func TestAgentToolPolicyWithWorkspace_NoTeam(t *testing.T) {
	got := agentToolPolicyWithWorkspace(nil, false)
	if got != nil {
		t.Errorf("no team should return nil, got %+v", got)
	}
}

func TestAgentToolPolicyWithWorkspace_WithTeam_InjectsFileTools(t *testing.T) {
	got := agentToolPolicyWithWorkspace(nil, true)
	if got == nil {
		t.Fatal("with team should return non-nil policy")
	}
	required := []string{"read_file", "write_file", "list_files"}
	for _, tool := range required {
		found := false
		for _, a := range got.AlsoAllow {
			if a == tool {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q in AlsoAllow", tool)
		}
	}
}

func TestAgentToolPolicyWithWorkspace_NoDuplicates(t *testing.T) {
	p := &config.ToolPolicySpec{AlsoAllow: []string{"read_file"}}
	got := agentToolPolicyWithWorkspace(p, true)
	count := 0
	for _, a := range got.AlsoAllow {
		if a == "read_file" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("read_file should appear exactly once, got %d", count)
	}
}

// ─── buildTeamMD ─────────────────────────────────────────────────────────

func TestBuildTeamMD_LeadRole_ContainsWorkflow(t *testing.T) {
	selfID := uuid.New()
	team := &store.TeamData{
		Name:        "test-team",
		Description: "desc",
	}
	members := []store.TeamMemberData{
		{AgentID: selfID, Role: store.TeamRoleLead, AgentKey: "lead-agent"},
		{AgentID: uuid.New(), Role: store.TeamRoleMember, AgentKey: "member-1", DisplayName: "Member One"},
	}
	md := buildTeamMD(team, members, selfID)
	if !strings.Contains(md, "# Team: test-team") {
		t.Error("expected team name in header")
	}
	if !strings.Contains(md, "Role: lead") {
		t.Error("expected role in output")
	}
	if !strings.Contains(md, "team_tasks") {
		t.Error("expected team_tasks in lead workflow section")
	}
	if !strings.Contains(md, "Member One") {
		t.Error("expected member display name")
	}
}

func TestBuildTeamMD_MemberRole_NoTaskCreation(t *testing.T) {
	selfID := uuid.New()
	team := &store.TeamData{Name: "proj"}
	members := []store.TeamMemberData{
		{AgentID: selfID, Role: store.TeamRoleMember, AgentKey: "member-agent"},
	}
	md := buildTeamMD(team, members, selfID)
	if !strings.Contains(md, "Role: member") {
		t.Error("expected member role in output")
	}
	// Member section should not contain lead-only task graph instructions
	if strings.Contains(md, "Task Planning") {
		t.Error("Task Planning section should only appear for lead")
	}
}

func TestBuildTeamMD_ReviewerRole_ContainsApproveInstructions(t *testing.T) {
	selfID := uuid.New()
	team := &store.TeamData{Name: "review-team"}
	members := []store.TeamMemberData{
		{AgentID: selfID, Role: store.TeamRoleReviewer, AgentKey: "reviewer-1"},
	}
	md := buildTeamMD(team, members, selfID)
	if !strings.Contains(md, "APPROVED") {
		t.Error("expected APPROVED/REJECTED guidance for reviewer")
	}
}

func TestBuildTeamMD_EmptyMembers(t *testing.T) {
	selfID := uuid.New()
	team := &store.TeamData{Name: "solo"}
	md := buildTeamMD(team, nil, selfID)
	// selfID not in members → defaults to member role
	if !strings.Contains(md, "# Team: solo") {
		t.Error("expected team name")
	}
}

func TestBuildTeamMD_LeadSeesReviewersSection(t *testing.T) {
	selfID := uuid.New()
	reviewerID := uuid.New()
	team := &store.TeamData{Name: "full-team"}
	members := []store.TeamMemberData{
		{AgentID: selfID, Role: store.TeamRoleLead, AgentKey: "lead"},
		{AgentID: reviewerID, Role: store.TeamRoleReviewer, AgentKey: "rev-1", DisplayName: "RevBot"},
	}
	md := buildTeamMD(team, members, selfID)
	if !strings.Contains(md, "Reviewers") {
		t.Error("lead should see Reviewers section")
	}
	if !strings.Contains(md, "RevBot") {
		t.Error("expected reviewer display name")
	}
}
