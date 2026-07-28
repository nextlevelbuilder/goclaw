package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type teamWorkGateTestEmbedder struct {
	called bool
}

func (e *teamWorkGateTestEmbedder) Name() string  { return "test-embedder" }
func (e *teamWorkGateTestEmbedder) Model() string { return "test-embedding" }
func (e *teamWorkGateTestEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.called = true
	return [][]float32{{1, 0}}, nil
}

type teamWorkGateTestProvider struct {
	called  bool
	model   string
	req     providers.ChatRequest
	content string
	err     error
}

func TestApplyTeamWorkGateDisabledLeavesGenericTeamFlowUntouched(t *testing.T) {
	disabled := false
	provider := &teamWorkGateTestProvider{}
	out := applyTeamWorkGateForInbound(context.Background(), &ConsumerDeps{
		Cfg: &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &disabled}},
	}, bus.InboundMessage{Content: "Ask the whole team to do this"}, "session", "agent", "direct", uuid.New(), nil, provider, "test-model", "run:test")
	if out.Message != "Ask the whole team to do this" || out.Directive != nil || out.DisableTeamWork || len(out.BlockedTools) != 0 {
		t.Fatalf("disabled gate changed generic team flow: %+v", out)
	}
	if provider.called {
		t.Fatal("disabled gate called the classifier provider")
	}
}

func (p *teamWorkGateTestProvider) Name() string { return "test-provider" }
func (p *teamWorkGateTestProvider) DefaultModel() string {
	if p.model != "" {
		return p.model
	}
	return "test-model"
}
func (p *teamWorkGateTestProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	if p.content != "" {
		return &providers.ChatResponse{Content: p.content}, nil
	}
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	switch {
	case strings.Contains(system, "Resolve the current user message into a complete standalone request"):
		return &providers.ChatResponse{Content: `{"standalone_request":"fix API","relation":"new","user_intent":"fix the API","inherited_scope":[],"requested_deliverables":["working API"],"quality_requirements":[],"explicit_constraints":[],"ambiguities":[],"needs_clarification":false}`}, nil
	case strings.Contains(system, "Independently verify that the draft standalone request"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	case strings.Contains(system, "decompose one already-resolved standalone user request"):
		return &providers.ChatResponse{Content: `{"workflow_mode":"self","independent_review_required":false,"reason":"one bounded task","work_units":[{"id":"fix","description":"fix API","required_output":"working API"}],"dependencies":[],"required_outputs":["working API"]}`}, nil
	case strings.Contains(system, "independently critique a proposed execution assignment"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[]}`}, nil
	default:
		return &providers.ChatResponse{Content: `{"workflow_mode":"self","current_agent_role":"Technical","task_type":"dev","current_agent_fit":"strong","best_team_owner":"","best_team_owner_role":"","best_team_fit":"none","specialist_match_found":false,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match","owner_selection_reason":"current agent owns dev work","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"self","required_tool":"","reason":"current agent owns the task"}`}, nil
	}
}

func newTeamWorkGateTestSkillsLoader(t *testing.T) *skills.Loader {
	t.Helper()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "skills", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: workflow\ndescription: test workflow\n---\nPINNED_WORKFLOW_BODY"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return skills.NewLoader(workspace, "", "")
}
func (p *teamWorkGateTestProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	p.called = true
	return nil, nil
}

func TestApplyTeamWorkGateForInboundSkipsAgentWithoutTeamOrDelegateLink(t *testing.T) {
	enabled := true
	embedder := &teamWorkGateTestEmbedder{}
	provider := &teamWorkGateTestProvider{}

	out := applyTeamWorkGateForInbound(context.Background(), &ConsumerDeps{
		Cfg:              &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}},
		TeamWorkEmbedder: embedder,
	}, bus.InboundMessage{
		Content:  "lập kế hoạch content và chiến lược cho chiến dịch mới",
		Metadata: map[string]string{},
	}, "session:test", "bao-an", "direct", uuid.New(), nil, provider, "test-model", "run:test")

	if out.Message != "lập kế hoạch content và chiến lược cho chiến dịch mới" {
		t.Fatalf("Message = %q, want original message", out.Message)
	}
	if out.Directive != nil {
		t.Fatalf("Directive = %+v, want nil", out.Directive)
	}
	if embedder.called {
		t.Fatal("embedder was called even though agent has no team/delegate capability")
	}
	if provider.called {
		t.Fatal("provider was called even though agent has no team/delegate capability")
	}
}

type teamWorkGateAgentStore struct {
	store.AgentStore
	agent *store.AgentData
}

func (s *teamWorkGateAgentStore) GetByID(context.Context, uuid.UUID) (*store.AgentData, error) {
	return s.agent, nil
}

func (s *teamWorkGateAgentStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	if s.agent == nil {
		return nil, nil
	}
	return []store.AgentData{*s.agent}, nil
}

type teamWorkGateTeamStore struct {
	store.TeamStore
	team      *store.TeamData
	members   []store.TeamMemberData
	auditErr  error
	lastAudit *store.TeamWorkClassificationAudit
}

func (s *teamWorkGateTeamStore) GetTeamForAgent(context.Context, uuid.UUID) (*store.TeamData, error) {
	return s.team, nil
}

func (s *teamWorkGateTeamStore) ListMembers(context.Context, uuid.UUID) ([]store.TeamMemberData, error) {
	return s.members, nil
}

// RecordTeamWorkClassificationAudit satisfies the audit write invoked on the
// gate path before scheduling; the fake stamps an ID and captures the row so a
// test can assert the linkable ID. auditErr forces a write failure to exercise
// the audit-before-schedule fail-safe.
func (s *teamWorkGateTeamStore) RecordTeamWorkClassificationAudit(_ context.Context, audit *store.TeamWorkClassificationAudit) error {
	if s.auditErr != nil {
		return s.auditErr
	}
	if audit.ID == uuid.Nil {
		audit.ID = uuid.New()
	}
	s.lastAudit = audit
	return nil
}

func TestApplyTeamWorkGateForInboundUsesClassifierOverrideProvider(t *testing.T) {
	enabled := true
	tenantID := uuid.New()
	agentID := uuid.New()
	teamID := uuid.New()
	agentProvider := &teamWorkGateTestProvider{}
	overrideProvider := &teamWorkGateTestProvider{model: "classifier-default"}
	registry := providers.NewRegistry(store.TenantIDFromContext)
	registry.RegisterForTenant(tenantID, overrideProvider)
	ctx := store.WithTenantID(context.Background(), tenantID)
	skillsLoader := newTeamWorkGateTestSkillsLoader(t)

	out := applyTeamWorkGateForInbound(ctx, &ConsumerDeps{
		Cfg: &config.Config{Gateway: config.GatewayConfig{
			TeamWorkClassify:         &enabled,
			TeamWorkClassifyProvider: overrideProvider.Name(),
			TeamWorkClassifyModel:    "classifier-model",
		}},
		AgentStore: &teamWorkGateAgentStore{agent: &store.AgentData{
			AgentKey:         "khanh-developer",
			DisplayName:      "Khanh Developer",
			AgentDescription: "Technical developer",
			OtherConfig:      []byte(`{"pinned_skills":["workflow"]}`),
		}},
		TeamStore: &teamWorkGateTeamStore{
			team: &store.TeamData{
				BaseModel:   store.BaseModel{ID: teamID},
				Name:        "growth-team",
				LeadAgentID: agentID,
			},
			members: []store.TeamMemberData{{
				TeamID:           teamID,
				AgentID:          agentID,
				AgentKey:         "khanh-developer",
				DisplayName:      "Khanh Developer",
				Role:             "Technical",
				AgentDescription: "Technical developer",
			}},
		},
		ProviderReg:      registry,
		TeamWorkEmbedder: &teamWorkGateTestEmbedder{},
		SkillsLoader:     skillsLoader,
	}, bus.InboundMessage{
		Content:  "fix API",
		Metadata: map[string]string{},
	}, "session:test", "khanh-developer", "direct", agentID, nil, agentProvider, "agent-model", "run:test")

	if out.Message != "fix API" {
		t.Fatalf("Message = %q, want original message", out.Message)
	}
	if !overrideProvider.called {
		t.Fatal("override provider was not used for team work classification")
	}
	if agentProvider.called {
		t.Fatal("agent provider was used despite valid classifier override")
	}
	if len(overrideProvider.req.Messages) < 2 || !strings.Contains(overrideProvider.req.Messages[1].Content, "PINNED_WORKFLOW_BODY") {
		t.Fatalf("classifier request missing pinned skill context: %+v", overrideProvider.req.Messages)
	}
}

func TestApplyTeamWorkGateForInboundDisablesOrchestrationOnClassifierFailure(t *testing.T) {
	enabled := true
	tenantID := uuid.New()
	agentID := uuid.New()
	teamID := uuid.New()
	tests := []struct {
		name     string
		provider *teamWorkGateTestProvider
	}{
		{name: "parse", provider: &teamWorkGateTestProvider{content: "not-json"}},
		{name: "transport", provider: &teamWorkGateTestProvider{err: errors.New("provider unavailable")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := applyTeamWorkGateForInbound(store.WithTenantID(context.Background(), tenantID), &ConsumerDeps{
				Cfg: &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}},
				AgentStore: &teamWorkGateAgentStore{agent: &store.AgentData{
					BaseModel: store.BaseModel{ID: agentID}, TenantID: tenantID,
					AgentKey: "khanh-developer", DisplayName: "Khanh Developer",
				}},
				TeamStore: &teamWorkGateTeamStore{
					team:    &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "growth-team", LeadAgentID: agentID, LeadAgentKey: "khanh-developer"},
					members: []store.TeamMemberData{{TeamID: teamID, AgentID: agentID, AgentKey: "khanh-developer", Role: "lead"}},
				},
				TeamWorkEmbedder: &teamWorkGateTestEmbedder{},
			}, bus.InboundMessage{Content: "fix API", Metadata: map[string]string{}}, "session:test", "khanh-developer", "direct", agentID, nil, tc.provider, "test-model", "run:test")

			if out.Directive != nil || !out.DisableTeamWork {
				t.Fatalf("gate outcome = %+v, want no directive and run-level disable", out)
			}
			for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
				if !slices.Contains(out.BlockedTools, tool) {
					t.Fatalf("blocked tools = %v, missing %s", out.BlockedTools, tool)
				}
			}
		})
	}
}

func TestApplyTeamWorkGateForInboundClassifiesWithoutEmbedder(t *testing.T) {
	enabled := true
	tenantID, agentID, teamID := uuid.New(), uuid.New(), uuid.New()
	provider := &teamWorkGateTestProvider{content: "not-json"}
	out := applyTeamWorkGateForInbound(store.WithTenantID(context.Background(), tenantID), &ConsumerDeps{
		Cfg: &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}},
		AgentStore: &teamWorkGateAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: agentID}, TenantID: tenantID, AgentKey: "lead",
		}},
		TeamStore: &teamWorkGateTeamStore{
			team:    &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, LeadAgentID: agentID, LeadAgentKey: "lead"},
			members: []store.TeamMemberData{{TeamID: teamID, AgentID: agentID, AgentKey: "lead", Role: "lead"}},
		},
	}, bus.InboundMessage{Content: "plan this", Metadata: map[string]string{}}, "session:test", "lead", "direct", agentID, nil, provider, "test-model", "run:test")

	if !provider.called || out.Directive != nil || !out.DisableTeamWork {
		t.Fatalf("gate outcome = %+v provider_called=%t, want classified degraded run", out, provider.called)
	}
}
