package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestBuildSystemPrompt_TeamContextInjection(t *testing.T) {
	teamMD := bootstrap.ContextFile{Path: bootstrap.TeamFile, Content: "# Team: Test\nRole: member"}
	availMD := bootstrap.ContextFile{Path: bootstrap.AvailabilityFile, Content: "You are NOT part of any team."}

	teamMembers := []store.TeamMemberData{
		{AgentKey: "leader-bot", DisplayName: "Leader", Role: store.TeamRoleLead},
		{AgentKey: "worker-bot", DisplayName: "Worker", Role: store.TeamRoleMember},
	}

	tests := []struct {
		name      string
		cfg       SystemPromptConfig
		wantIn    []string
		wantNotIn []string
	}{
		{
			name: "leader inbound chat — team sections present, no spawn",
			cfg: SystemPromptConfig{
				IsTeamContext: true,
				HasSpawn:      true,
				ToolNames:     []string{"team_tasks", "spawn", "read_file"},
				TeamWorkspace: "/app/workspace/teams/test",
				TeamMembers:   teamMembers,
				ContextFiles:  []bootstrap.ContextFile{teamMD},
				AgentType:     store.AgentTypePredefined,
			},
			wantIn:    []string{"Team Shared Workspace", "Team Members"},
			wantNotIn: []string{"Sub-Agent Spawning"},
		},
		{
			name: "member-only inbound chat — spawn present, no team sections",
			cfg: SystemPromptConfig{
				IsTeamContext: false,
				HasSpawn:      true,
				ToolNames:     []string{"spawn", "read_file"},
				ContextFiles:  []bootstrap.ContextFile{},
				AgentType:     store.AgentTypePredefined,
			},
			wantIn:    []string{"Sub-Agent Spawning"},
			wantNotIn: []string{"Team Shared Workspace", "Team Members"},
		},
		{
			name: "team dispatch (member) — team sections present, no spawn",
			cfg: SystemPromptConfig{
				IsTeamContext: true,
				HasSpawn:      true,
				ToolNames:     []string{"team_tasks", "spawn", "read_file"},
				TeamWorkspace: "/app/workspace/teams/test",
				TeamMembers:   teamMembers,
				ContextFiles:  []bootstrap.ContextFile{teamMD},
				AgentType:     store.AgentTypePredefined,
			},
			wantIn:    []string{"Team Shared Workspace", "Team Members"},
			wantNotIn: []string{"Sub-Agent Spawning"},
		},
		{
			name: "solo agent (no team) — spawn present, availability note",
			cfg: SystemPromptConfig{
				IsTeamContext: false,
				HasSpawn:      true,
				ToolNames:     []string{"spawn", "read_file"},
				ContextFiles:  []bootstrap.ContextFile{availMD},
				AgentType:     store.AgentTypeOpen,
			},
			wantIn:    []string{"Sub-Agent Spawning", "NOT part of any team"},
			wantNotIn: []string{"Team Shared Workspace", "Team Members"},
		},
		{
			name: "leader + bootstrap — team skipped due to bootstrap",
			cfg: SystemPromptConfig{
				IsTeamContext: true,
				IsBootstrap:   true,
				HasSpawn:      true,
				ToolNames:     []string{"write_file"},
				TeamWorkspace: "/app/workspace/teams/test",
				TeamMembers:   teamMembers,
				ContextFiles: []bootstrap.ContextFile{
					teamMD,
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				AgentType: store.AgentTypeOpen,
			},
			wantNotIn: []string{"Team Shared Workspace", "Sub-Agent Spawning"},
		},
		{
			name: "member + minimal mode (subagent) — no team, no spawn",
			cfg: SystemPromptConfig{
				IsTeamContext: false,
				HasSpawn:      false,
				Mode:          PromptMinimal,
				ToolNames:     []string{"read_file"},
				ContextFiles:  []bootstrap.ContextFile{},
				AgentType:     store.AgentTypePredefined,
			},
			wantNotIn: []string{"Team Shared Workspace", "Sub-Agent Spawning", "Team Members"},
		},
		{
			name: "leader + no spawn tool — team present, no spawn section",
			cfg: SystemPromptConfig{
				IsTeamContext: true,
				HasSpawn:      false,
				ToolNames:     []string{"team_tasks", "read_file"},
				TeamWorkspace: "/app/workspace/teams/test",
				TeamMembers:   teamMembers,
				ContextFiles:  []bootstrap.ContextFile{teamMD},
				AgentType:     store.AgentTypePredefined,
			},
			wantIn:    []string{"Team Shared Workspace", "Team Members"},
			wantNotIn: []string{"Sub-Agent Spawning"},
		},
		{
			name: "member-only with spawn — spawn guidance present",
			cfg: SystemPromptConfig{
				IsTeamContext: false,
				HasSpawn:      true,
				ToolNames:     []string{"spawn", "read_file", "write_file"},
				ContextFiles:  []bootstrap.ContextFile{},
				AgentType:     store.AgentTypeOpen,
			},
			wantIn:    []string{"Sub-Agent Spawning", "MUST spawn one per item"},
			wantNotIn: []string{"Team Shared Workspace"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildSystemPrompt(tt.cfg)
			for _, want := range tt.wantIn {
				if !strings.Contains(prompt, want) {
					t.Errorf("expected prompt to contain %q", want)
				}
			}
			for _, notWant := range tt.wantNotIn {
				if strings.Contains(prompt, notWant) {
					t.Errorf("expected prompt NOT to contain %q", notWant)
				}
			}
		})
	}
}
