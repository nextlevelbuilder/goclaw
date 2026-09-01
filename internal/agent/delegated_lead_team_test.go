package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// mockTeamStoreLead serves ListTeams and nothing else; the embedded interface is
// nil on purpose so an unexpected store call fails loudly instead of silently.
type mockTeamStoreLead struct {
	store.TeamStore
	teams []store.TeamData
	err   error
}

func (m *mockTeamStoreLead) ListTeams(context.Context) ([]store.TeamData, error) {
	return m.teams, m.err
}

func leadTeam(id, lead uuid.UUID, status string, settings string) store.TeamData {
	td := store.TeamData{
		BaseModel:   store.BaseModel{ID: id},
		LeadAgentID: lead,
		Status:      status,
	}
	if settings != "" {
		td.Settings = json.RawMessage(settings)
	}
	return td
}

// newLeadTestLoop mirrors newArtifactTestLoop but wires a team store and pins the
// agent identity, so the resolver has something to resolve against.
func newLeadTestLoop(root string, agentID uuid.UUID, teams store.TeamStore) *Loop {
	return NewLoop(LoopConfig{
		ID:        "brain",
		AgentUUID: agentID,
		TenantID:  store.MasterTenantID,
		Workspace: filepath.Join(root, "agents", "brain"),
		DataDir:   root,
		Sessions:  &nopSessionStore{},
		TeamStore: teams,
	})
}

func TestResolveDelegatedLeadTeamRead(t *testing.T) {
	root := t.TempDir()
	lead := uuid.New()
	other := uuid.New()
	teamA := uuid.New()
	teamB := uuid.New()
	req := &RunRequest{ChatID: "chat-1", WorkspaceChatID: "chat-1"}

	t.Run("SingleLedTeamResolves", func(t *testing.T) {
		s := &mockTeamStoreLead{teams: []store.TeamData{
			leadTeam(teamA, lead, store.TeamStatusActive, ""),
			leadTeam(teamB, other, store.TeamStatusActive, ""),
		}}
		got := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req)
		if !got.ok() {
			t.Fatalf("nothing resolved for an agent leading exactly one team: %#v", got)
		}
		if !strings.Contains(got.workspace, teamA.String()) {
			t.Errorf("workspace %q does not belong to the led team %s", got.workspace, teamA)
		}
		if strings.Contains(got.workspace, teamB.String()) || strings.Contains(got.root, teamB.String()) {
			t.Errorf("resolved paths leak into an unrelated team: %#v", got)
		}
		// Isolated is the default: the workspace is a chat-scoped leaf below the
		// root, and the root is what makes a peer's chat scope reachable at all.
		if got.workspace != filepath.Join(got.root, "chat-1") {
			t.Errorf("workspace %q is not the chat leaf of root %q", got.workspace, got.root)
		}
	})

	t.Run("SharedTeamCollapsesToRoot", func(t *testing.T) {
		s := &mockTeamStoreLead{teams: []store.TeamData{
			leadTeam(teamA, lead, store.TeamStatusActive, `{"workspace_scope":"shared"}`),
		}}
		got := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req)
		if !got.ok() || got.workspace != got.root {
			t.Errorf("a shared team must resolve workspace == root, got %#v", got)
		}
	})

	t.Run("MultipleLedTeamsResolveNothing", func(t *testing.T) {
		s := &mockTeamStoreLead{teams: []store.TeamData{
			leadTeam(teamA, lead, store.TeamStatusActive, ""),
			leadTeam(teamB, lead, store.TeamStatusActive, ""),
		}}
		got := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req)
		if got.ok() {
			t.Errorf("resolved %#v for an agent leading two teams; the delegation does not say "+
				"which team it is about, so guessing would hand out the wrong workspace", got)
		}
	})

	t.Run("NonLeadResolvesNothing", func(t *testing.T) {
		s := &mockTeamStoreLead{teams: []store.TeamData{
			leadTeam(teamA, other, store.TeamStatusActive, ""),
		}}
		if got := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req); got.ok() {
			t.Errorf("resolved %#v for an agent that leads no team", got)
		}
	})

	t.Run("InactiveTeamIgnored", func(t *testing.T) {
		s := &mockTeamStoreLead{teams: []store.TeamData{leadTeam(teamA, lead, "archived", "")}}
		if got := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req); got.ok() {
			t.Errorf("resolved %#v from an inactive team", got)
		}
	})

	t.Run("StoreFailureResolvesNothing", func(t *testing.T) {
		s := &mockTeamStoreLead{err: errors.New("db down")}
		if got := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req); got.ok() {
			t.Errorf("resolved %#v despite a store failure", got)
		}
	})

	t.Run("NoTeamStoreResolvesNothing", func(t *testing.T) {
		if got := newLeadTestLoop(root, lead, nil).resolveDelegatedLeadTeamRead(context.Background(), req); got.ok() {
			t.Errorf("resolved %#v without a team store", got)
		}
	})

	t.Run("SeparateLeadsGetSeparateWorkspaces", func(t *testing.T) {
		s := &mockTeamStoreLead{teams: []store.TeamData{
			leadTeam(teamA, lead, store.TeamStatusActive, ""),
			leadTeam(teamB, other, store.TeamStatusActive, ""),
		}}
		a := newLeadTestLoop(root, lead, s).resolveDelegatedLeadTeamRead(context.Background(), req)
		b := newLeadTestLoop(root, other, s).resolveDelegatedLeadTeamRead(context.Background(), req)
		if !a.ok() || !b.ok() || a.workspace == b.workspace || a.root == b.root {
			t.Errorf("leads of different teams must resolve different paths, got %#v and %#v", a, b)
		}
	})
}

// The guard at the top of the artifact branch rejects req.TeamWorkspace outright,
// and the team-workspace application below it is gated on !isArtifactDelegation.
// Granting the lead read access must therefore happen inside the artifact branch
// and must not touch the active workspace: this test fails both if the run is
// refused and if the exchange outputs directory is displaced.
func TestInjectContext_DelegatedLeadReadsTeamWithoutDisplacingOutputs(t *testing.T) {
	root := t.TempDir()
	leadID := uuid.New()
	teamID := uuid.New()
	s := &mockTeamStoreLead{teams: []store.TeamData{
		leadTeam(teamID, leadID, store.TeamStatusActive, ""),
	}}
	req := newArtifactRunRequest(t, root)
	req.ChatID = "chat-1"
	req.WorkspaceChatID = "chat-1"

	setup, err := newLeadTestLoop(root, leadID, s).injectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("delegation setup refused: %v", err)
	}
	if got := tools.ToolWorkspaceFromCtx(setup.ctx); got != req.DelegateOutputsPath {
		t.Fatalf("active workspace = %q, want the exchange outputs %q — the team workspace "+
			"must be readable, never the run's own workspace", got, req.DelegateOutputsPath)
	}
	if got := tools.DelegationArtifactInputsFromCtx(setup.ctx); got != req.DelegateInputsPath {
		t.Fatalf("artifact inputs = %q, want %q", got, req.DelegateInputsPath)
	}
	wantWs := filepath.Join(root, "teams", teamID.String(), "chat-1")
	if got := tools.ToolTeamWorkspaceFromCtx(setup.ctx); got != wantWs {
		t.Errorf("team workspace = %q, want %q", got, wantWs)
	}
	wantRoot := filepath.Join(root, "teams", teamID.String())
	if got := tools.ToolTeamRootFromCtx(setup.ctx); got != wantRoot {
		t.Errorf("team root = %q, want %q", got, wantRoot)
	}
	// Read access only: a team ID in context switches on the workspace
	// interceptor's write validation and event broadcast, which the delegated
	// run has no business triggering.
	if got := tools.ToolTeamIDFromCtx(setup.ctx); got != "" {
		t.Errorf("team ID = %q, want empty in a delegation artifact run", got)
	}
}

// An agent that leads no team must come out of a delegated run exactly as before.
func TestInjectContext_DelegatedNonLeadGainsNothing(t *testing.T) {
	root := t.TempDir()
	s := &mockTeamStoreLead{teams: []store.TeamData{
		leadTeam(uuid.New(), uuid.New(), store.TeamStatusActive, ""),
	}}
	req := newArtifactRunRequest(t, root)

	setup, err := newLeadTestLoop(root, uuid.New(), s).injectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("injectContext: %v", err)
	}
	if got := tools.ToolTeamWorkspaceFromCtx(setup.ctx); got != "" {
		t.Errorf("team workspace = %q, want empty for an agent leading no team", got)
	}
	if got := tools.ToolTeamRootFromCtx(setup.ctx); got != "" {
		t.Errorf("team root = %q, want empty for an agent leading no team", got)
	}
}
