package methods

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// chatIngressMsg is echoed verbatim by the fixture provider as the resolved
// standalone request, so the shape verifier's trait evidence (the same string)
// is literally present in the request it validates against.
const chatIngressMsg = "draft one onboarding email for new signups"

// chatIngressOrchestratingProvider drives the full production classifier pipeline
// (intent resolve -> intent critic -> shape verify -> decompose -> plan ->
// assignment critic) to a validated single_owner TEAM decision — the minimal
// orchestrating outcome (team directive, no frozen plan). It dispatches on each
// stage's system prompt exactly like the store-layer pgTeamWorkProvider.
type chatIngressOrchestratingProvider struct{ called bool }

func (p *chatIngressOrchestratingProvider) Name() string         { return "ingress-orchestrating" }
func (p *chatIngressOrchestratingProvider) DefaultModel() string { return "test-model" }
func (p *chatIngressOrchestratingProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("streaming not supported")
}
func (p *chatIngressOrchestratingProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	switch {
	case strings.Contains(system, "Resolve the current user message into a complete standalone request"):
		return &providers.ChatResponse{Content: `{"standalone_request":"` + chatIngressMsg + `","relation":"new","user_intent":"draft email","inherited_scope":[],"requested_deliverables":["email"],"quality_requirements":[],"explicit_constraints":[],"ambiguities":[],"needs_clarification":false}`}, nil
	case strings.Contains(system, "Independently verify that the draft standalone request"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	case strings.Contains(system, "You independently verify the semantic work shape"):
		return &providers.ChatResponse{Content: `{"work_shape":"atomic","shape_traits":[{"type":"single_bounded_output","source":"current_request","evidence":"` + chatIngressMsg + `"}],"independent_review_required":false}`}, nil
	case strings.Contains(system, "decompose one already-resolved standalone user request"):
		return &providers.ChatResponse{Content: `{"workflow_mode":"single_owner","independent_review_required":false,"reason":"one specialist unit","work_units":[{"id":"draft","description":"draft email","required_output":"email"}],"dependencies":[],"required_outputs":["email"]}`}, nil
	case strings.Contains(system, "independently critique a proposed execution assignment"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[]}`}, nil
	default:
		return &providers.ChatResponse{Content: `{"workflow_mode":"single_owner","current_agent_role":"lead","task_type":"content","current_agent_fit":"partial","best_team_owner":"content-owner","best_team_owner_role":"content","best_team_fit":"strong","specialist_match_found":true,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match","owner_selection_reason":"owner drafts","followup_context_used_for_reference_only":false,"workflow_executable":true,"decision":"team","required_tool":"team_tasks","reason":"route to owner","plan":null}`}, nil
	}
}

// The three tool-store fakes below are the minimum needed for
// applyAvailableToolSnapshots to resolve a KNOWN-positive team_tasks availability
// for the coordinator, so workflowExecutability passes and a team decision can
// form. Without a known-positive required tool the classifier fails safe to self
// and the audit-vs-orchestration distinction under test would never arise.
type chatIngressBuiltinToolStore struct{ store.BuiltinToolStore }

func (chatIngressBuiltinToolStore) List(context.Context) ([]store.BuiltinToolDef, error) {
	return []store.BuiltinToolDef{{Name: "team_tasks", Enabled: true}}, nil
}

type chatIngressTenantToolStore struct {
	store.BuiltinToolTenantConfigStore
}

func (chatIngressTenantToolStore) ListAll(context.Context, uuid.UUID) (map[string]bool, error) {
	return map[string]bool{}, nil
}

type chatIngressMCPStore struct{}

func (chatIngressMCPStore) ListServers(context.Context) ([]store.MCPServerData, error) {
	return nil, nil
}
func (chatIngressMCPStore) ListAgentGrantsByAgentIDs(context.Context, []uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}

// wireOrchestratingChatMethods builds a ChatMethods wired so applyTeamWorkGate
// yields a validated single_owner TEAM decision for chatIngressMsg. teamStore is
// injected by the caller so it can carry an auditErr (or not).
func wireOrchestratingChatMethods(t *testing.T, teamStore *chatTeamWorkTeamStore, leadID, tenantID uuid.UUID) (*ChatMethods, *chatIngressOrchestratingProvider) {
	t.Helper()
	enabled := true
	methods := NewChatMethods(nil, nil, &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}}, nil, nil)
	methods.SetTeamWorkClassification(
		&chatTeamWorkAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: leadID}, TenantID: tenantID,
			AgentKey: "team-lead", DisplayName: "Team Lead",
		}},
		teamStore,
		nil, // linkStore
		&chatTeamWorkTestEmbedder{},
		nil, // skillsLoader
		chatIngressMCPStore{},
		chatIngressBuiltinToolStore{},
		chatIngressTenantToolStore{},
		tools.NewPolicyEngine(&config.ToolsConfig{}),
		tools.NewRegistry(),
	)
	return methods, &chatIngressOrchestratingProvider{}
}

func orchestratingChatTeamStore(auditErr error, leadID, teamID uuid.UUID) *chatTeamWorkTeamStore {
	ownerID := uuid.New()
	return &chatTeamWorkTeamStore{
		team: &store.TeamData{
			BaseModel:    store.BaseModel{ID: teamID},
			Name:         "growth-team",
			LeadAgentID:  leadID,
			LeadAgentKey: "team-lead",
		},
		members: []store.TeamMemberData{
			{TeamID: teamID, AgentID: leadID, AgentKey: "team-lead", Role: "lead"},
			{TeamID: teamID, AgentID: ownerID, AgentKey: "content-owner", DisplayName: "Content Owner", Role: "member", AgentDescription: "drafts content"},
		},
		auditErr: auditErr,
	}
}

// A durable audit WRITE FAILURE on a real orchestrating decision at the WS
// ingress must flip the gate outcome closed: no directive, team work disabled for
// the run, and the canonical orchestration tools blocked. This exercises
// applyTeamWorkGate directly, so it proves the audit-before-gate-returns fail-safe
// at the WS ingress on a fully-wired orchestrating decision — not just in the
// agent.BuildAuditedTeamWorkGateDecision helper.
//
// SCOPE: this asserts the GATE OUTPUT (chatTeamWorkGateOutcome). That the WS
// dispatcher then copies out.disableTeamWork/out.blockedTools/out.directive into
// agent.RunRequest is STATIC-VERIFIED in chat.go (TeamWorkDirective/DisableTeamWork/
// BlockedTools at chat.go:506-508, audit ID threaded via
// tools.WithTeamWorkClassificationAuditID at chat.go:452). The full
// ingress → RunRequest → tool → workflow round trip is a Phase 10 E2E, not asserted
// here.
func TestChatMethodsApplyTeamWorkGateAuditFailureBlocksOrchestration(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingChatTeamStore(errors.New("audit db down"), leadID, teamID)
	methods, provider := wireOrchestratingChatMethods(t, teamStore, leadID, tenantID)

	out := methods.applyTeamWorkGate(
		store.WithTenantID(context.Background(), tenantID),
		chatSendParams{Message: chatIngressMsg, AgentID: "team-lead"},
		&chatTeamWorkTestAgent{id: leadID, provider: provider, model: "test-model"},
		"session:test",
		"run:test",
	)

	if !provider.called {
		t.Fatal("classifier provider was never called; the gate short-circuited before classifying")
	}
	if out.directive != nil {
		t.Fatalf("audit write failure must drop the orchestrating directive, got %+v", out.directive)
	}
	if !out.disableTeamWork {
		t.Fatal("audit write failure must disable team work for the run")
	}
	if out.auditID != uuid.Nil {
		t.Fatalf("failed audit write must yield no audit ID, got %s", out.auditID)
	}
	for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
		if !slices.Contains(out.blockedTools, tool) {
			t.Fatalf("blocked tools = %v, missing %s", out.blockedTools, tool)
		}
	}
}

// The same wiring with a SUCCESSFUL audit write keeps orchestration and returns a
// non-nil audit ID — the value the WS dispatcher threads into the run context via
// tools.WithTeamWorkClassificationAuditID so a workflow created during the run
// links back to this decision. Guards against the fixture merely always failing.
func TestChatMethodsApplyTeamWorkGateSuccessReturnsAuditID(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingChatTeamStore(nil, leadID, teamID)
	methods, provider := wireOrchestratingChatMethods(t, teamStore, leadID, tenantID)

	out := methods.applyTeamWorkGate(
		store.WithTenantID(context.Background(), tenantID),
		chatSendParams{Message: chatIngressMsg, AgentID: "team-lead"},
		&chatTeamWorkTestAgent{id: leadID, provider: provider, model: "test-model"},
		"session:test",
		"run:test",
	)

	if out.directive == nil {
		t.Fatal("successful audit on a team decision must keep the orchestrating directive")
	}
	if out.disableTeamWork || len(out.blockedTools) != 0 {
		t.Fatalf("successful orchestration must not disable team work or block tools: %+v", out)
	}
	if out.auditID == uuid.Nil {
		t.Fatal("successful audit write must return a non-nil audit ID for context threading")
	}
	if teamStore.lastAudit == nil || teamStore.lastAudit.ID != out.auditID {
		t.Fatalf("returned audit ID must match the persisted row: id=%s captured=%+v", out.auditID, teamStore.lastAudit)
	}
	if teamStore.lastAudit.EffectiveMode != store.TeamWorkModeSingleOwner {
		t.Fatalf("audit must record the single_owner effective mode, got %q", teamStore.lastAudit.EffectiveMode)
	}
}
