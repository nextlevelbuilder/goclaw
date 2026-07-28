package teamworkclassify

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type rosterAgentStore struct {
	store.AgentStore
	agents    map[uuid.UUID]store.AgentData
	getByIDsN int
}

func (s *rosterAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	agent := s.agents[id]
	return &agent, nil
}

func (s *rosterAgentStore) GetByIDs(_ context.Context, ids []uuid.UUID) ([]store.AgentData, error) {
	s.getByIDsN++
	result := make([]store.AgentData, 0, len(ids))
	for _, id := range ids {
		if agent, ok := s.agents[id]; ok {
			result = append(result, agent)
		}
	}
	return result, nil
}

type rosterTeamStore struct {
	store.TeamStore
	team           store.TeamData
	members        []store.TeamMemberData
	teams          map[uuid.UUID]store.TeamData
	membersByTeam  map[uuid.UUID][]store.TeamMemberData
	teamForAgentID uuid.UUID
}

func (s *rosterTeamStore) GetTeamForAgent(context.Context, uuid.UUID) (*store.TeamData, error) {
	if team, ok := s.teams[s.teamForAgentID]; ok {
		return &team, nil
	}
	return &s.team, nil
}

func (s *rosterTeamStore) GetTeam(_ context.Context, teamID uuid.UUID) (*store.TeamData, error) {
	if team, ok := s.teams[teamID]; ok {
		return &team, nil
	}
	return &s.team, nil
}

func (s *rosterTeamStore) ListMembers(_ context.Context, teamID uuid.UUID) ([]store.TeamMemberData, error) {
	if members, ok := s.membersByTeam[teamID]; ok {
		return members, nil
	}
	return s.members, nil
}

type rosterBuiltinStore struct {
	store.BuiltinToolStore
	listN int
}

func (s *rosterBuiltinStore) List(context.Context) ([]store.BuiltinToolDef, error) {
	s.listN++
	return []store.BuiltinToolDef{{Name: "read_file", Enabled: true}, {Name: "write_file", Enabled: true}, {Name: "team_tasks", Enabled: true}}, nil
}

type rosterTenantToolStore struct {
	store.BuiltinToolTenantConfigStore
	listN int
}

func (s *rosterTenantToolStore) ListAll(context.Context, uuid.UUID) (map[string]bool, error) {
	s.listN++
	return map[string]bool{"write_file": false}, nil
}

type rosterMCPStore struct {
	serverListN int
	grantListN  int
	server      store.MCPServerData
	grants      []store.MCPAgentGrant
}

func (s *rosterMCPStore) ListServers(context.Context) ([]store.MCPServerData, error) {
	s.serverListN++
	return []store.MCPServerData{s.server}, nil
}

func (s *rosterMCPStore) ListAgentGrantsByAgentIDs(context.Context, []uuid.UUID) ([]store.MCPAgentGrant, error) {
	s.grantListN++
	return s.grants, nil
}

func TestBuildInputFromStoresBuildsRosterAndToolSnapshotWithoutNPlusOne(t *testing.T) {
	tenantID := uuid.New()
	teamID := uuid.New()
	leadID := uuid.New()
	memberID := uuid.New()
	serverID := uuid.New()
	agents := &rosterAgentStore{agents: map[uuid.UUID]store.AgentData{
		leadID: {
			BaseModel: store.BaseModel{ID: leadID}, TenantID: tenantID, AgentKey: "lead", DisplayName: "Lead", Provider: "openai",
			OtherConfig: json.RawMessage(`{"capabilities":["lead_coordinator"]}`), AgentDescription: "coordinates work",
		},
		memberID: {
			BaseModel: store.BaseModel{ID: memberID}, TenantID: tenantID, AgentKey: "researcher", DisplayName: "Researcher", Provider: "openai",
			OtherConfig: json.RawMessage(`{"capabilities":[{"key":"research","label":"Market research"}]}`), AgentDescription: "research expertise",
		},
	}}
	teams := &rosterTeamStore{
		team: store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "Research", LeadAgentID: leadID, LeadAgentKey: "lead"},
		members: []store.TeamMemberData{
			{TeamID: teamID, AgentID: leadID, AgentKey: "lead", DisplayName: "Lead", Role: "lead"},
			{TeamID: teamID, AgentID: memberID, AgentKey: "researcher", DisplayName: "Researcher", Role: "member"},
		},
	}
	builtins := &rosterBuiltinStore{}
	tenantTools := &rosterTenantToolStore{}
	mcpStore := &rosterMCPStore{
		server: store.MCPServerData{BaseModel: store.BaseModel{ID: serverID}, Name: "market-data", Enabled: true},
		grants: []store.MCPAgentGrant{{AgentID: memberID, ServerID: serverID, Enabled: true, ToolAllow: json.RawMessage(`["search_market"]`)}},
	}
	input := BuildInputFromStores(store.WithTenantID(context.Background(), tenantID), ProfileStores{
		Agents: agents, Teams: teams, MCP: mcpStore, BuiltinTools: builtins, TenantToolConfigs: tenantTools,
		ToolPolicy: tools.NewPolicyEngine(&config.ToolsConfig{}), ToolRegistry: tools.NewRegistry(),
	}, BuildInputOptions{Mode: ModeTeam, AgentID: leadID, Message: "research market"})

	if agents.getByIDsN != 1 || builtins.listN != 1 || tenantTools.listN != 1 || mcpStore.serverListN != 1 || mcpStore.grantListN != 1 {
		t.Fatalf("batch calls: agents=%d builtin=%d tenant=%d servers=%d grants=%d", agents.getByIDsN, builtins.listN, tenantTools.listN, mcpStore.serverListN, mcpStore.grantListN)
	}
	if input.CoordinatorAgentID != leadID || input.CoordinatorAgentKey != "lead" {
		t.Fatalf("coordinator = %s/%s", input.CoordinatorAgentID, input.CoordinatorAgentKey)
	}
	member, ok := canonicalTeamProfile(input, memberID, "researcher")
	if !ok || member.CapabilitiesStatus != DataStatusKnown || !profileHasStructuredCapability(member, "research") {
		t.Fatalf("member roster = %+v", member)
	}
	if member.AvailableToolsStatus != DataStatusKnown || !slicesContains(member.AvailableTools, "read_file") || !slicesContains(member.AvailableTools, "mcp_market_data__search_market") || slicesContains(member.AvailableTools, "write_file") {
		t.Fatalf("member tools = %v status=%s", member.AvailableTools, member.AvailableToolsStatus)
	}
	if executable, reason := workflowExecutability(input); !executable || reason != "" {
		t.Fatalf("workflow executability = %t/%q, want executable", executable, reason)
	}

	restricted := BuildInputFromStores(store.WithTenantID(context.Background(), tenantID), ProfileStores{
		Agents: agents, Teams: teams, MCP: mcpStore, BuiltinTools: builtins, TenantToolConfigs: tenantTools,
		ToolPolicy: tools.NewPolicyEngine(&config.ToolsConfig{}), ToolRegistry: tools.NewRegistry(),
	}, BuildInputOptions{Mode: ModeTeam, AgentID: leadID, Message: "research market", ToolAllow: []string{"read_file"}})
	if executable, reason := workflowExecutability(restricted); executable || reason != "required_tool_unavailable" {
		t.Fatalf("channel-restricted executability = %t/%q, want required_tool_unavailable", executable, reason)
	}
}

func TestBuildInputFromStoresBindsExplicitTeamWhenCoordinatorLeadsMultipleTeams(t *testing.T) {
	tenantID := uuid.New()
	teamAID, teamBID := uuid.New(), uuid.New()
	leadID, memberAID, memberBID := uuid.New(), uuid.New(), uuid.New()
	agents := &rosterAgentStore{agents: map[uuid.UUID]store.AgentData{
		leadID:    {BaseModel: store.BaseModel{ID: leadID}, TenantID: tenantID, AgentKey: "lead"},
		memberAID: {BaseModel: store.BaseModel{ID: memberAID}, TenantID: tenantID, AgentKey: "team-a-worker"},
		memberBID: {BaseModel: store.BaseModel{ID: memberBID}, TenantID: tenantID, AgentKey: "team-b-worker"},
	}}
	teams := &rosterTeamStore{
		teams: map[uuid.UUID]store.TeamData{
			teamAID: {BaseModel: store.BaseModel{ID: teamAID}, Name: "Team A", LeadAgentID: leadID, LeadAgentKey: "lead"},
			teamBID: {BaseModel: store.BaseModel{ID: teamBID}, Name: "Team B", LeadAgentID: leadID, LeadAgentKey: "lead"},
		},
		teamForAgentID: teamBID,
		membersByTeam: map[uuid.UUID][]store.TeamMemberData{
			teamAID: {
				{TeamID: teamAID, AgentID: leadID, AgentKey: "lead", Role: store.TeamRoleLead},
				{TeamID: teamAID, AgentID: memberAID, AgentKey: "team-a-worker", Role: store.TeamRoleMember},
			},
			teamBID: {
				{TeamID: teamBID, AgentID: leadID, AgentKey: "lead", Role: store.TeamRoleLead},
				{TeamID: teamBID, AgentID: memberBID, AgentKey: "team-b-worker", Role: store.TeamRoleMember},
			},
		},
	}

	input := BuildInputFromStores(store.WithTenantID(context.Background(), tenantID), ProfileStores{
		Agents: agents, Teams: teams,
	}, BuildInputOptions{Mode: ModeTeam, AgentID: leadID, TeamID: teamAID})

	if input.Team.Name != "Team A" || input.CoordinatorAgentID != leadID {
		t.Fatalf("explicit team input = %+v", input.Team)
	}
	if _, ok := canonicalTeamProfile(input, memberAID, "team-a-worker"); !ok {
		t.Fatal("explicit Team A member is missing")
	}
	if _, ok := canonicalTeamProfile(input, memberBID, "team-b-worker"); ok {
		t.Fatal("agent-selected Team B member leaked into Team A roster")
	}
}

// Unknown ABSENCE of the orchestration required tool is not availability: a
// snapshot that cannot confirm team_tasks fails safe to self rather than
// assuming the tool is present.
func TestWorkflowExecutabilityRejectsUnknownCurrentToolAbsence(t *testing.T) {
	input := plannerTestInput()
	input.CurrentAgent.AvailableToolsStatus = DataStatusUnknown
	input.CurrentAgent.AvailableTools = nil
	if executable, reason := workflowExecutability(input); executable || reason != "required_tool_unavailable" {
		t.Fatalf("workflow executability = %t/%q, want required_tool_unavailable for unknown absence", executable, reason)
	}
}

// Positive evidence still passes even when the snapshot status is unknown because
// an UNRELATED tool source could not be enumerated: the required tool is listed,
// so it is genuinely available.
func TestWorkflowExecutabilityAcceptsPresentToolDespiteUnknownStatus(t *testing.T) {
	input := plannerTestInput()
	input.CurrentAgent.AvailableToolsStatus = DataStatusUnknown
	input.CurrentAgent.AvailableTools = []string{"team_tasks"}
	if executable, reason := workflowExecutability(input); !executable || reason != "" {
		t.Fatalf("workflow executability = %t/%q, want executable on positive tool evidence", executable, reason)
	}
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
