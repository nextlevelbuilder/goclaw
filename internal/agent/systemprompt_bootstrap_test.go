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
			name: "predefined agent first run uses softened get-to-know copy",
			cfg: SystemPromptConfig{
				IsBootstrap: false,
				AgentType:   store.AgentTypePredefined,
				ContextFiles: []bootstrap.ContextFile{
					{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
				},
				ToolNames: []string{"write_file", "web_search"},
			},
			wantIn:    "GET TO KNOW THE USER",
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

// TestBuildSystemPrompt_PredefinedBootstrapSoftened verifies that the
// predefined+BOOTSTRAP.md branch no longer forces write_file on turn 1.
// Context: Gemini 3 with thinking_level=low exhausted its 8192-token budget
// before emitting tool args, resulting in write_file({}) → HTTP 400.
// Removing the MUST-call-write_file-this-turn mandate lets small models
// respond conversationally when they lack real user info to write.
// Trace: 019d8f33-2de1-7ab2-9a32-9df92cd610dd.
func TestBuildSystemPrompt_PredefinedBootstrapSoftened(t *testing.T) {
	baseCfg := func(channel string) SystemPromptConfig {
		return SystemPromptConfig{
			IsBootstrap: false,
			AgentType:   store.AgentTypePredefined,
			Channel:     channel,
			ContextFiles: []bootstrap.ContextFile{
				{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
			},
			ToolNames: []string{"write_file", "web_search"},
		}
	}

	tests := []struct {
		name       string
		cfg        SystemPromptConfig
		wantIn     []string
		wantNotIn  []string
	}{
		{
			name: "A: ws channel uses softened copy",
			cfg:  baseCfg("ws"),
			wantIn: []string{
				"GET TO KNOW THE USER",
			},
			wantNotIn: []string{
				"before your response ends",
				"MUST ALSO call write_file",
			},
		},
		{
			name: "B: forbids empty/placeholder args explicitly",
			cfg:  baseCfg("ws"),
			wantIn: []string{
				"Do NOT call write_file",
				"empty or placeholder",
			},
		},
		{
			name: "C: forbids session-identifier content",
			cfg:  baseCfg("ws"),
			wantIn: []string{
				"never copy session identifiers",
			},
		},
		{
			name: "D: telegram channel uses same softened copy (uniform)",
			cfg:  baseCfg("telegram"),
			wantIn: []string{
				"GET TO KNOW THE USER",
			},
			wantNotIn: []string{
				"before your response ends",
				"MUST ALSO call write_file",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildSystemPrompt(tt.cfg)
			for _, want := range tt.wantIn {
				if !strings.Contains(prompt, want) {
					t.Errorf("expected %q in prompt", want)
				}
			}
			for _, notWant := range tt.wantNotIn {
				if strings.Contains(prompt, notWant) {
					t.Errorf("unexpected %q in prompt (must be removed)", notWant)
				}
			}
		})
	}
}

// TestBuildSystemPrompt_OpenBootstrapUnchanged verifies Phase 04 did NOT
// touch the open-agent slim branch — its existing mandate copy stays.
func TestBuildSystemPrompt_OpenBootstrapUnchanged(t *testing.T) {
	cfg := SystemPromptConfig{
		IsBootstrap: true,
		AgentType:   store.AgentTypeOpen,
		ContextFiles: []bootstrap.ContextFile{
			{Path: bootstrap.BootstrapFile, Content: "# BOOTSTRAP"},
		},
		ToolNames: []string{"write_file"},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Do NOT give a generic greeting") {
		t.Error("open-bootstrap branch must keep its existing mandate copy")
	}
	if !strings.Contains(prompt, "only have write_file available") {
		t.Error("open-bootstrap branch must keep its tool-limit note")
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
