package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// routingTeamStore records the settings written by handleUpdate and serves a
// fixed member roster. Any method not overridden panics via the nil embed.
type routingTeamStore struct {
	store.TeamStore
	team      *store.TeamData
	members   []store.TeamMemberData
	lastSet   map[string]any
	createdTm *store.TeamData
	hasAccess bool
	updated   bool
}

func (s *routingTeamStore) HasTeamAccess(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return s.hasAccess, nil
}

func (s *routingTeamStore) GetTeam(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return s.team, nil
}

func (s *routingTeamStore) GetTeamForAgent(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return nil, nil
}

func (s *routingTeamStore) ListMembers(_ context.Context, _ uuid.UUID) ([]store.TeamMemberData, error) {
	return s.members, nil
}

func (s *routingTeamStore) UpdateTeam(_ context.Context, _ uuid.UUID, updates map[string]any) error {
	s.updated = true
	s.lastSet = updates
	return nil
}

func (s *routingTeamStore) CreateTeam(_ context.Context, team *store.TeamData) error {
	team.ID = uuid.New()
	s.createdTm = team
	return nil
}

func (s *routingTeamStore) AddMember(_ context.Context, _, _ uuid.UUID, _ string) error {
	return nil
}

// routingAgentStore resolves agent keys for teams.create.
type routingAgentStore struct {
	store.AgentStore
}

func (s *routingAgentStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	return &store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, AgentKey: key}, nil
}

// callRPC runs handler and returns the single response frame's ok flag,
// error code, and raw payload.
func callRPC(t *testing.T, handler func(context.Context, *gateway.Client, *protocol.RequestFrame), params map[string]any) (bool, string, json.RawMessage) {
	t.Helper()
	return callRPCAs(t, permissions.RoleAdmin, handler, params)
}

func callRPCAs(t *testing.T, role permissions.Role, handler func(context.Context, *gateway.Client, *protocol.RequestFrame), params map[string]any) (bool, string, json.RawMessage) {
	t.Helper()
	client, frames := gateway.NewCapturingTestClient(role, uuid.Nil, "op-1", 4)
	raw, _ := json.Marshal(params)
	handler(context.Background(), client, &protocol.RequestFrame{ID: "1", Params: raw})
	select {
	case b := <-frames:
		var resp struct {
			OK      bool            `json:"ok"`
			Payload json.RawMessage `json:"payload"`
			Error   *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			t.Fatalf("bad frame: %v", err)
		}
		if resp.Error != nil {
			return resp.OK, resp.Error.Code, resp.Payload
		}
		return resp.OK, "", resp.Payload
	default:
		t.Fatal("no response frame")
		return false, "", nil
	}
}

func newRoutingUpdateFixture(agentKeys ...string) (*routingTeamStore, *TeamsMethods) {
	st := &routingTeamStore{team: &store.TeamData{BaseModel: store.BaseModel{ID: uuid.New()}}}
	for _, k := range agentKeys {
		st.members = append(st.members, store.TeamMemberData{AgentKey: k})
	}
	return st, &TeamsMethods{teamStore: st}
}

func updateParams(teamID uuid.UUID, settings map[string]any) map[string]any {
	return map[string]any{"teamId": teamID.String(), "settings": settings}
}

func TestTeamsUpdatePreservesRoutingBlock(t *testing.T) {
	st, m := newRoutingUpdateFixture("coder-agent")
	ok, code, _ := callRPC(t, m.handleUpdate, updateParams(st.team.ID, map[string]any{
		"routing": map[string]any{
			"enabled":              true,
			"confidence_threshold": 0.8,
			"rules": []map[string]any{
				{"agent_key": "coder-agent", "keywords": []string{"go"}, "file_patterns": []string{".go"}},
			},
		},
	}))
	if !ok {
		t.Fatalf("update rejected: %s", code)
	}
	settings, _ := st.lastSet["settings"].(json.RawMessage)
	var round struct {
		Routing struct {
			Enabled             *bool    `json:"enabled"`
			ConfidenceThreshold *float64 `json:"confidence_threshold"`
			Rules               []struct {
				AgentKey string   `json:"agent_key"`
				Keywords []string `json:"keywords"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(settings, &round); err != nil {
		t.Fatalf("settings not JSON: %v", err)
	}
	if round.Routing.Enabled == nil || !*round.Routing.Enabled {
		t.Fatalf("routing.enabled lost on save: %s", settings)
	}
	if round.Routing.ConfidenceThreshold == nil || *round.Routing.ConfidenceThreshold != 0.8 {
		t.Fatalf("routing.confidence_threshold lost on save: %s", settings)
	}
	if len(round.Routing.Rules) != 1 || round.Routing.Rules[0].AgentKey != "coder-agent" ||
		len(round.Routing.Rules[0].Keywords) != 1 || round.Routing.Rules[0].Keywords[0] != "go" {
		t.Fatalf("routing rules lost on save: %s", settings)
	}
}

func TestTeamsUpdateStripsUnknownKeys(t *testing.T) {
	st, m := newRoutingUpdateFixture("coder-agent")
	ok, code, _ := callRPC(t, m.handleUpdate, updateParams(st.team.ID, map[string]any{
		"bogus_key": "x",
		"routing": map[string]any{
			"enabled": true,
			"rules":   []map[string]any{{"agent_key": "coder-agent"}},
		},
	}))
	if !ok {
		t.Fatalf("update rejected: %s", code)
	}
	settings, _ := st.lastSet["settings"].(json.RawMessage)
	var round map[string]json.RawMessage
	if err := json.Unmarshal(settings, &round); err != nil {
		t.Fatalf("settings not JSON: %v", err)
	}
	if _, present := round["bogus_key"]; present {
		t.Fatalf("unknown key not stripped: %s", settings)
	}
	if _, present := round["routing"]; !present {
		t.Fatalf("routing dropped alongside unknown key: %s", settings)
	}
}

func TestTeamsUpdateRejectsInvalidRouting(t *testing.T) {
	st, m := newRoutingUpdateFixture("coder-agent")
	cases := map[string]map[string]any{
		"threshold zero":     {"routing": map[string]any{"confidence_threshold": 0}},
		"threshold above 1":  {"routing": map[string]any{"confidence_threshold": 1.5}},
		"threshold negative": {"routing": map[string]any{"confidence_threshold": -0.1}},
		"empty agent_key":    {"routing": map[string]any{"rules": []map[string]any{{"keywords": []string{"go"}}}}},
		"blank agent_key":    {"routing": map[string]any{"rules": []map[string]any{{"agent_key": "   "}}}},
	}
	for name, settings := range cases {
		st.lastSet = nil
		ok, code, _ := callRPC(t, m.handleUpdate, updateParams(st.team.ID, settings))
		if ok || code != protocol.ErrInvalidRequest {
			t.Fatalf("%s: expected INVALID_REQUEST, got ok=%v code=%q", name, ok, code)
		}
		if st.lastSet != nil {
			t.Fatalf("%s: invalid write reached UpdateTeam", name)
		}
	}
}

func TestTeamsUpdateSanitizesStaleRoutingRules(t *testing.T) {
	st, m := newRoutingUpdateFixture("coder-agent")
	// Save contains a rule for current member "coder-agent" and a rule for departed member "removed-agent"
	ok, code, _ := callRPC(t, m.handleUpdate, updateParams(st.team.ID, map[string]any{
		"routing": map[string]any{
			"enabled": true,
			"rules": []map[string]any{
				{"agent_key": "coder-agent", "keywords": []string{"go"}},
				{"agent_key": "removed-agent", "keywords": []string{"python"}},
			},
		},
	}))
	if !ok {
		t.Fatalf("update with stale rule should succeed after sanitization, got code=%s", code)
	}
	settings, _ := st.lastSet["settings"].(json.RawMessage)
	var round struct {
		Routing struct {
			Rules []struct {
				AgentKey string `json:"agent_key"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(settings, &round); err != nil {
		t.Fatalf("settings not JSON: %v", err)
	}
	if len(round.Routing.Rules) != 1 || round.Routing.Rules[0].AgentKey != "coder-agent" {
		t.Fatalf("expected stale rule to be dropped, got rules: %+v", round.Routing.Rules)
	}
}

func TestTeamsCreatePassesRoutingThrough(t *testing.T) {
	st := &routingTeamStore{}
	m := &TeamsMethods{teamStore: st, agentStore: &routingAgentStore{}}
	settings := map[string]any{
		"routing": map[string]any{
			"enabled": true,
			"rules":   []map[string]any{{"agent_key": "a", "keywords": []string{"go"}}},
		},
	}
	ok, code, _ := callRPC(t, m.handleCreate, map[string]any{
		"name":     "t",
		"lead":     "l",
		"members":  []string{"m"},
		"settings": settings,
	})
	if !ok {
		t.Fatalf("create rejected: %s", code)
	}
	if st.createdTm == nil {
		t.Fatal("CreateTeam not called")
	}
	var round struct {
		Routing struct {
			Enabled *bool `json:"enabled"`
			Rules   []struct {
				AgentKey string   `json:"agent_key"`
				Keywords []string `json:"keywords"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(st.createdTm.Settings, &round); err != nil {
		t.Fatalf("created settings not JSON: %v", err)
	}
	if round.Routing.Enabled == nil || !*round.Routing.Enabled ||
		len(round.Routing.Rules) != 1 || round.Routing.Rules[0].AgentKey != "a" ||
		len(round.Routing.Rules[0].Keywords) != 1 || round.Routing.Rules[0].Keywords[0] != "go" {
		t.Fatalf("routing mangled on create: %s", st.createdTm.Settings)
	}
}

func TestTeamsUpdateRequiresTeamAccessForNonAdmin(t *testing.T) {
	cases := []struct {
		name       string
		hasAccess  bool
		wantOK     bool
		wantUpdate bool
	}{
		{"non-member non-admin is denied", false, false, false},
		{"member non-admin is allowed", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, m := newRoutingUpdateFixture("coder-agent")
			st.hasAccess = tc.hasAccess
			ok, code, _ := callRPCAs(t, permissions.RoleOperator, m.handleUpdate, updateParams(st.team.ID, map[string]any{
				"routing": map[string]any{"enabled": true, "rules": []map[string]any{{"agent_key": "coder-agent"}}},
			}))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v (code %q), want %v", ok, code, tc.wantOK)
			}
			if !tc.wantOK && code != protocol.ErrNotFound {
				t.Fatalf("code = %q, want %q (must not leak existence)", code, protocol.ErrNotFound)
			}
			if st.updated != tc.wantUpdate {
				t.Fatalf("UpdateTeam called = %v, want %v", st.updated, tc.wantUpdate)
			}
		})
	}
}

func TestTeamsUpdateAdminBypassesTeamAccess(t *testing.T) {
	st, m := newRoutingUpdateFixture("coder-agent")
	st.hasAccess = false
	ok, code, _ := callRPC(t, m.handleUpdate, updateParams(st.team.ID, map[string]any{
		"routing": map[string]any{"enabled": true, "rules": []map[string]any{{"agent_key": "coder-agent"}}},
	}))
	if !ok {
		t.Fatalf("admin update rejected: %s", code)
	}
	if !st.updated {
		t.Fatal("admin update did not reach UpdateTeam")
	}
}
