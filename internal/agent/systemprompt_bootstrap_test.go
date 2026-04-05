package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestBuildSystemPrompt_BootstrapStates verifies the 4 bootstrap states
// produce the correct system prompt sections.
func TestBuildSystemPrompt_BootstrapStates(t *testing.T) {
	blankUserMD := "# USER.md\n\n- **Name:**\n- **Language:**\n- **Timezone:**\n"
	populatedUserMD := "# USER.md\n\n- **Name:** Alice\n- **Language:** English\n- **Timezone:** UTC+7\n"

	tests := []struct {
		name       string
		cfg        SystemPromptConfig
		wantIn     string // substring that MUST appear
		wantNotIn  string // substring that MUST NOT appear (empty = skip check)
	}{
		{
			name: "open agent with BOOTSTRAP.md → FIRST RUN slim mode",
			cfg: SystemPromptConfig{
				IsBootstrap: true,
				AgentType:   store.AgentTypeOpen,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file", "Write"},
			},
			wantIn:    "## FIRST RUN",
			wantNotIn: "USER PROFILE INCOMPLETE",
		},
		{
			name: "predefined agent with BOOTSTRAP.md → FIRST RUN full capabilities",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file", "Write", "skill_search"},
			},
			wantIn:    "## FIRST RUN",
			wantNotIn: "USER PROFILE INCOMPLETE",
		},
		{
			name: "no BOOTSTRAP.md + blank USER.md → USER PROFILE INCOMPLETE",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.UserFile, Content: blankUserMD},
				},
				ToolNames: []string{"write_file"},
			},
			wantIn:    "## USER PROFILE INCOMPLETE",
			wantNotIn: "FIRST RUN",
		},
		{
			name: "no BOOTSTRAP.md + populated USER.md → no nudge at all",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.UserFile, Content: populatedUserMD},
				},
				ToolNames: []string{"write_file"},
			},
			wantNotIn: "FIRST RUN",
		},
		{
			name: "open agent slim mode has write_file note",
			cfg: SystemPromptConfig{
				IsBootstrap: true,
				AgentType:   store.AgentTypeOpen,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				ToolNames: []string{"write_file"},
			},
			wantIn: "only have write_file available",
		},
		{
			name: "predefined agent first run has write_file instruction",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				ToolNames: []string{"write_file", "web_search"},
			},
			wantIn:    "MUST ALSO call write_file",
			wantNotIn: "only have write_file available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildSystemPrompt(tt.cfg)

			if tt.wantIn != "" && !strings.Contains(prompt, tt.wantIn) {
				t.Errorf("expected %q in system prompt, got:\n%s", tt.wantIn, prompt[:min(len(prompt), 500)])
			}
			if tt.wantNotIn != "" && strings.Contains(prompt, tt.wantNotIn) {
				t.Errorf("unexpected %q in system prompt", tt.wantNotIn)
			}

			// Always verify: populated USER.md must never trigger INCOMPLETE
			if tt.name == "no BOOTSTRAP.md + populated USER.md → no nudge at all" {
				if strings.Contains(prompt, "USER PROFILE INCOMPLETE") {
					t.Error("populated USER.md should not trigger USER PROFILE INCOMPLETE")
				}
			}
		})
	}
}

// TestBuildSystemPrompt_NoBootstrapNoUser verifies that when there are no
// bootstrap-related files at all, no nudge sections appear.
func TestBuildSystemPrompt_NoBootstrapNoUser(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{
		AgentType: store.AgentTypePredefined,
		ToolNames: []string{"write_file"},
	})

	if strings.Contains(prompt, "FIRST RUN") {
		t.Error("unexpected FIRST RUN section with no context files")
	}
	if strings.Contains(prompt, "USER PROFILE INCOMPLETE") {
		t.Error("unexpected USER PROFILE INCOMPLETE section with no context files")
	}
}
