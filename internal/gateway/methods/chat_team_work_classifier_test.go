package methods

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type chatTeamWorkTestProvider struct {
	called  bool
	calls   int
	model   string
	req     providers.ChatRequest
	content string
}

func (p *chatTeamWorkTestProvider) Name() string { return "test-provider" }
func (p *chatTeamWorkTestProvider) DefaultModel() string {
	if p.model != "" {
		return p.model
	}
	return "test-model"
}
func (p *chatTeamWorkTestProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	p.calls++
	p.req = req
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

func newChatTeamWorkTestSkillsLoader(t *testing.T) *skills.Loader {
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
func (p *chatTeamWorkTestProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	p.called = true
	return nil, nil
}

type chatTeamWorkTestAgent struct {
	id       uuid.UUID
	provider providers.Provider
	model    string
}

func (a *chatTeamWorkTestAgent) ID() string                   { return "khanh-developer" }
func (a *chatTeamWorkTestAgent) UUID() uuid.UUID              { return a.id }
func (a *chatTeamWorkTestAgent) OtherConfig() json.RawMessage { return nil }
func (a *chatTeamWorkTestAgent) Run(context.Context, agent.RunRequest) (*agent.RunResult, error) {
	return nil, nil
}
func (a *chatTeamWorkTestAgent) IsRunning() bool              { return false }
func (a *chatTeamWorkTestAgent) Model() string                { return a.model }
func (a *chatTeamWorkTestAgent) ProviderName() string         { return a.provider.Name() }
func (a *chatTeamWorkTestAgent) Provider() providers.Provider { return a.provider }

type chatTeamWorkAgentStore struct {
	store.AgentStore
	agent *store.AgentData
}

func (s *chatTeamWorkAgentStore) GetByID(context.Context, uuid.UUID) (*store.AgentData, error) {
	return s.agent, nil
}

func (s *chatTeamWorkAgentStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	if s.agent == nil {
		return nil, nil
	}
	return []store.AgentData{*s.agent}, nil
}

type chatTeamWorkTeamStore struct {
	store.TeamStore
	team      *store.TeamData
	members   []store.TeamMemberData
	auditErr  error
	lastAudit *store.TeamWorkClassificationAudit
}

func (s *chatTeamWorkTeamStore) GetTeamForAgent(context.Context, uuid.UUID) (*store.TeamData, error) {
	return s.team, nil
}

func (s *chatTeamWorkTeamStore) ListMembers(context.Context, uuid.UUID) ([]store.TeamMemberData, error) {
	return s.members, nil
}

// RecordTeamWorkClassificationAudit records the audit write invoked on the gate
// path before scheduling. The fake captures the last audit and returns success
// so the gate proceeds; auditErr forces a write failure to exercise fail-safe.
func (s *chatTeamWorkTeamStore) RecordTeamWorkClassificationAudit(_ context.Context, audit *store.TeamWorkClassificationAudit) error {
	if s.auditErr != nil {
		return s.auditErr
	}
	if audit.ID == uuid.Nil {
		audit.ID = uuid.New()
	}
	s.lastAudit = audit
	return nil
}

func TestChatMethodsApplyTeamWorkGateUsesClassifierOverrideProvider(t *testing.T) {
	enabled := true
	tenantID := uuid.New()
	agentID := uuid.New()
	teamID := uuid.New()
	agentProvider := &chatTeamWorkTestProvider{}
	overrideProvider := &chatTeamWorkTestProvider{model: "classifier-default"}
	registry := providers.NewRegistry(store.TenantIDFromContext)
	registry.RegisterForTenant(tenantID, overrideProvider)
	cfg := &config.Config{Gateway: config.GatewayConfig{
		TeamWorkClassify:         &enabled,
		TeamWorkClassifyProvider: overrideProvider.Name(),
		TeamWorkClassifyModel:    "classifier-model",
	}}
	methods := NewChatMethods(nil, nil, cfg, nil, nil)
	skillsLoader := newChatTeamWorkTestSkillsLoader(t)
	methods.SetProviderRegistry(registry)
	methods.SetTeamWorkClassification(
		&chatTeamWorkAgentStore{agent: &store.AgentData{
			AgentKey:         "khanh-developer",
			DisplayName:      "Khanh Developer",
			AgentDescription: "Technical developer",
			OtherConfig:      []byte(`{"pinned_skills":["workflow"]}`),
		}},
		&chatTeamWorkTeamStore{
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
		nil,
		skillsLoader,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	out := methods.applyTeamWorkGate(
		store.WithTenantID(context.Background(), tenantID),
		chatSendParams{Message: "fix API", AgentID: "khanh-developer"},
		&chatTeamWorkTestAgent{id: agentID, provider: agentProvider, model: "agent-model"},
		"session:test",
		"run:test",
	)

	if out.message != "fix API" {
		t.Fatalf("message = %q, want original message", out.message)
	}
	if !overrideProvider.called {
		t.Fatal("override provider was not used for websocket team work classification")
	}
	if agentProvider.called {
		t.Fatal("agent provider was used despite valid websocket classifier override")
	}
	if len(overrideProvider.req.Messages) < 2 || !strings.Contains(overrideProvider.req.Messages[1].Content, "PINNED_WORKFLOW_BODY") {
		t.Fatalf("websocket classifier request missing pinned skill context: %+v", overrideProvider.req.Messages)
	}
}

func TestChatMethodsApplyTeamWorkGateDisablesOrchestrationOnClassifierParseFailure(t *testing.T) {
	enabled := true
	tenantID := uuid.New()
	agentID := uuid.New()
	teamID := uuid.New()
	provider := &chatTeamWorkTestProvider{content: "not-json"}
	methods := NewChatMethods(nil, nil, &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}}, nil, nil)
	methods.SetTeamWorkClassification(
		&chatTeamWorkAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: agentID}, TenantID: tenantID,
			AgentKey: "khanh-developer", DisplayName: "Khanh Developer",
		}},
		&chatTeamWorkTeamStore{
			team:    &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "growth-team", LeadAgentID: agentID, LeadAgentKey: "khanh-developer"},
			members: []store.TeamMemberData{{TeamID: teamID, AgentID: agentID, AgentKey: "khanh-developer", Role: "lead"}},
		},
		nil, nil, nil, nil, nil, nil, nil,
	)

	out := methods.applyTeamWorkGate(
		store.WithTenantID(context.Background(), tenantID),
		chatSendParams{Message: "fix API", AgentID: "khanh-developer"},
		&chatTeamWorkTestAgent{id: agentID, provider: provider, model: "test-model"},
		"session:test",
		"run:test",
	)
	if out.directive != nil || !out.disableTeamWork {
		t.Fatalf("gate outcome = %+v, want no directive and run-level disable", out)
	}
	for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
		if !slices.Contains(out.blockedTools, tool) {
			t.Fatalf("blocked tools = %v, missing %s", out.blockedTools, tool)
		}
	}
}

// TestChatMethodsApplyTeamWorkGateEnabledWithoutEmbeddingProvider proves the
// enabled toggle classifies on a deployment that has no embedding provider at
// all: routing is a single LLM call, so nothing in the gate consults an
// embedder. The classifier must be reached (exactly one provider call) and the
// message must pass through unchanged.
func TestChatMethodsApplyTeamWorkGateEnabledWithoutEmbeddingProvider(t *testing.T) {
	enabled := true
	tenantID, agentID, teamID := uuid.New(), uuid.New(), uuid.New()
	provider := &chatTeamWorkTestProvider{}
	methods := NewChatMethods(nil, nil, &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}}, nil, nil)
	methods.SetTeamWorkClassification(
		&chatTeamWorkAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: agentID}, TenantID: tenantID,
			AgentKey: "khanh-developer", DisplayName: "Khanh Developer",
		}},
		&chatTeamWorkTeamStore{
			team:    &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "growth-team", LeadAgentID: agentID, LeadAgentKey: "khanh-developer"},
			members: []store.TeamMemberData{{TeamID: teamID, AgentID: agentID, AgentKey: "khanh-developer", Role: "lead"}},
		},
		nil, nil, nil, nil, nil, nil, nil,
	)

	out := methods.applyTeamWorkGate(
		store.WithTenantID(context.Background(), tenantID),
		chatSendParams{Message: "fix API", AgentID: "khanh-developer"},
		&chatTeamWorkTestAgent{id: agentID, provider: provider, model: "test-model"},
		"session:test",
		"run:test",
	)
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want exactly 1 routing call with no embedding provider wired", provider.calls)
	}
	if out.message != "fix API" {
		t.Fatalf("message = %q, want original message", out.message)
	}
}

func TestChatMethodsApplyTeamWorkGateClassifiesWithMissingAuxiliaryStores(t *testing.T) {
	enabled := true
	tenantID, agentID, teamID := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name       string
		agentStore store.AgentStore
	}{
		// No embedding provider is wired anywhere in this package any more: the
		// classifier is a single LLM routing call, so classification must still
		// reach the provider with only the roster stores present.
		{name: "no embedding provider", agentStore: &chatTeamWorkAgentStore{agent: &store.AgentData{BaseModel: store.BaseModel{ID: agentID}, TenantID: tenantID, AgentKey: "lead"}}},
		{name: "missing agent store"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider := &chatTeamWorkTestProvider{content: "not-json"}
			methods := NewChatMethods(nil, nil, &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}}, nil, nil)
			methods.SetTeamWorkClassification(
				tc.agentStore,
				&chatTeamWorkTeamStore{
					team:    &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, LeadAgentID: agentID, LeadAgentKey: "lead"},
					members: []store.TeamMemberData{{TeamID: teamID, AgentID: agentID, AgentKey: "lead", Role: "lead"}},
				},
				nil, nil, nil, nil, nil, nil, nil,
			)
			out := methods.applyTeamWorkGate(
				store.WithTenantID(context.Background(), tenantID),
				chatSendParams{Message: "plan this", AgentID: "lead"},
				&chatTeamWorkTestAgent{id: agentID, provider: provider, model: "test-model"},
				"session:test",
				"run:test",
			)
			if !provider.called || out.directive != nil || !out.disableTeamWork {
				t.Fatalf("gate outcome = %+v provider_called=%t, want classified degraded run", out, provider.called)
			}
		})
	}
}
