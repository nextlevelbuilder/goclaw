package cmd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// inboundIngressMsg is echoed verbatim by the fixture provider as the resolved
// standalone request, so the shape verifier's trait evidence (the same string)
// is literally present in the request it validates against.
const inboundIngressMsg = "draft one onboarding email for new signups"

// inboundOrchestratingProvider drives the full production classifier pipeline to
// a validated single_owner TEAM decision — the minimal orchestrating outcome
// (team directive, no frozen plan). Dispatches on each stage's system prompt.
type inboundOrchestratingProvider struct{ called bool }

func (p *inboundOrchestratingProvider) Name() string         { return "ingress-orchestrating" }
func (p *inboundOrchestratingProvider) DefaultModel() string { return "test-model" }
func (p *inboundOrchestratingProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("streaming not supported")
}
func (p *inboundOrchestratingProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	switch {
	case strings.Contains(system, "Resolve the current user message into a complete standalone request"):
		return &providers.ChatResponse{Content: `{"standalone_request":"` + inboundIngressMsg + `","relation":"new","user_intent":"draft email","inherited_scope":[],"requested_deliverables":["email"],"quality_requirements":[],"explicit_constraints":[],"ambiguities":[],"needs_clarification":false}`}, nil
	case strings.Contains(system, "Independently verify that the draft standalone request"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	case strings.Contains(system, "You independently verify the semantic work shape"):
		return &providers.ChatResponse{Content: `{"work_shape":"atomic","shape_traits":[{"type":"single_bounded_output","source":"current_request","evidence":"` + inboundIngressMsg + `"}],"independent_review_required":false}`}, nil
	case strings.Contains(system, "decompose one already-resolved standalone user request"):
		return &providers.ChatResponse{Content: `{"workflow_mode":"single_owner","independent_review_required":false,"reason":"one specialist unit","work_units":[{"id":"draft","description":"draft email","required_output":"email"}],"dependencies":[],"required_outputs":["email"]}`}, nil
	case strings.Contains(system, "independently critique a proposed execution assignment"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[]}`}, nil
	default:
		return &providers.ChatResponse{Content: `{"workflow_mode":"single_owner","current_agent_role":"lead","task_type":"content","current_agent_fit":"partial","best_team_owner":"content-owner","best_team_owner_role":"content","best_team_fit":"strong","specialist_match_found":true,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match","owner_selection_reason":"owner drafts","followup_context_used_for_reference_only":false,"workflow_executable":true,"decision":"team","required_tool":"team_tasks","reason":"route to owner","plan":null}`}, nil
	}
}

// Minimal tool-store fakes so applyAvailableToolSnapshots resolves a KNOWN
// team_tasks availability for the coordinator; without a known-positive required
// tool the classifier fails safe to self and the case under test never arises.
type inboundBuiltinToolStore struct{ store.BuiltinToolStore }

func (inboundBuiltinToolStore) List(context.Context) ([]store.BuiltinToolDef, error) {
	return []store.BuiltinToolDef{{Name: "team_tasks", Enabled: true}}, nil
}

type inboundTenantToolStore struct {
	store.BuiltinToolTenantConfigStore
}

func (inboundTenantToolStore) ListAll(context.Context, uuid.UUID) (map[string]bool, error) {
	return map[string]bool{}, nil
}

type inboundMCPStore struct{}

func (inboundMCPStore) ListServers(context.Context) ([]store.MCPServerData, error) {
	return nil, nil
}
func (inboundMCPStore) ListAgentGrantsByAgentIDs(context.Context, []uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}

func orchestratingInboundTeamStore(auditErr error, leadID, teamID uuid.UUID) *teamWorkGateTeamStore {
	ownerID := uuid.New()
	return &teamWorkGateTeamStore{
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

func orchestratingInboundDeps(teamStore *teamWorkGateTeamStore, leadID, tenantID uuid.UUID) *ConsumerDeps {
	enabled := true
	return &ConsumerDeps{
		Cfg: &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}},
		AgentStore: &teamWorkGateAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: leadID}, TenantID: tenantID,
			AgentKey: "team-lead", DisplayName: "Team Lead",
		}},
		TeamStore:        teamStore,
		TeamWorkEmbedder: &teamWorkGateTestEmbedder{},
		MCPStore:         inboundMCPStore{},
		BuiltinToolStore: inboundBuiltinToolStore{},
		TenantToolStore:  inboundTenantToolStore{},
		ToolPolicy:       tools.NewPolicyEngine(&config.ToolsConfig{}),
		ToolRegistry:     tools.NewRegistry(),
	}
}

// A durable audit WRITE FAILURE on a real orchestrating decision at the inbound
// ingress must flip the gate outcome closed: no directive, team work disabled,
// canonical orchestration tools blocked. This exercises applyTeamWorkGateForInbound
// directly, so it proves the audit-before-gate-returns fail-safe at the inbound
// ingress on a fully-wired orchestrating decision — not just in the
// agent.BuildAuditedTeamWorkGateDecision helper.
//
// SCOPE: this asserts the GATE OUTPUT (teamWorkGateOutcome). In
// gateway_consumer_normal.go the gate now runs at DEQUEUE inside the RunRequest
// PreRun hook (Phase 7 review 7A-H1): that closure copies
// gate.Directive/DisableTeamWork/BlockedTools onto the dequeued *agent.RunRequest
// and threads gate.AuditID into the run context via
// tools.WithTeamWorkClassificationAuditID before loop.Run — so the classify+audit
// still precede the run (Phase 6 audit-before-run) while reading the latest
// history. The dequeue timing itself is covered by
// internal/scheduler/queue_prerun_test.go; the full ingress → RunRequest → tool →
// workflow round trip is a Phase 10 E2E, not asserted here.
func TestApplyTeamWorkGateForInboundAuditFailureBlocksOrchestration(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingInboundTeamStore(errors.New("audit db down"), leadID, teamID)
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	provider := &inboundOrchestratingProvider{}

	out := applyTeamWorkGateForInbound(
		store.WithTenantID(context.Background(), tenantID),
		deps,
		bus.InboundMessage{Content: inboundIngressMsg, Metadata: map[string]string{}},
		"session:test", "team-lead", "direct", leadID, nil, provider, "test-model", "run:test",
	)

	if !provider.called {
		t.Fatal("classifier provider was never called; the gate short-circuited before classifying")
	}
	if out.Directive != nil {
		t.Fatalf("audit write failure must drop the orchestrating directive, got %+v", out.Directive)
	}
	if !out.DisableTeamWork {
		t.Fatal("audit write failure must disable team work for the run")
	}
	if out.AuditID != uuid.Nil {
		t.Fatalf("failed audit write must yield no audit ID, got %s", out.AuditID)
	}
	for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
		if !slices.Contains(out.BlockedTools, tool) {
			t.Fatalf("blocked tools = %v, missing %s", out.BlockedTools, tool)
		}
	}
}

// The same wiring with a SUCCESSFUL audit write keeps orchestration and returns a
// non-nil audit ID — the value gateway_consumer_normal.go threads into schedCtx
// via tools.WithTeamWorkClassificationAuditID so a workflow created during the
// run links back to this decision.
func TestApplyTeamWorkGateForInboundSuccessReturnsAuditID(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingInboundTeamStore(nil, leadID, teamID)
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	provider := &inboundOrchestratingProvider{}

	out := applyTeamWorkGateForInbound(
		store.WithTenantID(context.Background(), tenantID),
		deps,
		bus.InboundMessage{Content: inboundIngressMsg, Metadata: map[string]string{}},
		"session:test", "team-lead", "direct", leadID, nil, provider, "test-model", "run:test",
	)

	if out.Directive == nil {
		t.Fatal("successful audit on a team decision must keep the orchestrating directive")
	}
	if out.DisableTeamWork || len(out.BlockedTools) != 0 {
		t.Fatalf("successful orchestration must not disable team work or block tools: %+v", out)
	}
	if out.AuditID == uuid.Nil {
		t.Fatal("successful audit write must return a non-nil audit ID for context threading")
	}
	if teamStore.lastAudit == nil || teamStore.lastAudit.ID != out.AuditID {
		t.Fatalf("returned audit ID must match the persisted row: id=%s captured=%+v", out.AuditID, teamStore.lastAudit)
	}
	if teamStore.lastAudit.Ingress != store.TeamWorkIngressInbound {
		t.Fatalf("audit must record the inbound ingress, got %q", teamStore.lastAudit.Ingress)
	}
}
