package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestOrchModeDenyTools_Spawn(t *testing.T) {
	deny := orchModeDenyTools(ModeSpawn)
	if !deny["delegate"] {
		t.Error("ModeSpawn should deny delegate")
	}
	if !deny["team_tasks"] {
		t.Error("ModeSpawn should deny team_tasks")
	}
}

func TestOrchModeDenyTools_Delegate(t *testing.T) {
	deny := orchModeDenyTools(ModeDelegate)
	if deny["delegate"] {
		t.Error("ModeDelegate should NOT deny delegate")
	}
	if !deny["team_tasks"] {
		t.Error("ModeDelegate should deny team_tasks")
	}
}

func TestOrchModeDenyTools_Team(t *testing.T) {
	deny := orchModeDenyTools(ModeTeam)
	if deny != nil {
		t.Errorf("ModeTeam should deny nothing, got %v", deny)
	}
}

func TestOrchModeDenyTools_ZeroValue(t *testing.T) {
	deny := orchModeDenyTools("")
	if deny != nil {
		t.Errorf("zero-value mode should deny nothing (permissive), got %v", deny)
	}
}

func TestResolveOrchestrationMode(t *testing.T) {
	agentID := uuid.New()
	tests := []struct {
		name     string
		team     *store.TeamData
		links    []store.AgentLinkData
		expected OrchestrationMode
	}{
		{
			name:     "has team -> ModeTeam",
			team:     &store.TeamData{Name: "test-team"},
			expected: ModeTeam,
		},
		{
			name:     "no team, has links -> ModeDelegate",
			links:    []store.AgentLinkData{{TargetAgentKey: "other"}},
			expected: ModeDelegate,
		},
		{
			name:     "no team, no links -> ModeSpawn",
			expected: ModeSpawn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &mockTeamStoreOrch{team: tt.team}
			ls := &mockLinkStoreOrch{targets: tt.links}
			got := ResolveOrchestrationMode(context.Background(), agentID, ts, ls)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResolveOrchestrationMode_NilStores(t *testing.T) {
	got := ResolveOrchestrationMode(context.Background(), uuid.New(), nil, nil)
	if got != ModeSpawn {
		t.Errorf("nil stores should resolve to ModeSpawn, got %q", got)
	}
}

// --- Minimal mock stores (only implement methods actually called) ---

type mockTeamStoreOrch struct {
	store.TeamStore // embed to satisfy interface; unused methods panic
	team            *store.TeamData
}

func (m *mockTeamStoreOrch) GetTeamForAgent(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return m.team, nil
}

type mockLinkStoreOrch struct {
	store.AgentLinkStore // embed to satisfy interface
	targets              []store.AgentLinkData
}

func (m *mockLinkStoreOrch) DelegateTargets(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return m.targets, nil
}
