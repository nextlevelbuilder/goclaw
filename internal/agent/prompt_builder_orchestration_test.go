package agent

import (
	"strings"
	"testing"
)

func TestBridgePromptBuilder_OrchestrationSection(t *testing.T) {
	builder := NewBridgePromptBuilder()

	tests := []struct {
		name        string
		cfg         PromptConfig
		wantContain string
		wantAbsent  bool
	}{
		{
			name: "orchestration enabled with targets",
			cfg: PromptConfig{
				Identity:      true,
				IdentityData:  IdentityData{AgentName: "test-agent"},
				Orchestration: true,
				OrchestrationData: OrchestrationSectionData{
					Mode: ModeDelegate,
					DelegateTargets: []DelegateTargetEntry{
						{AgentKey: "helper", DisplayName: "Helper Bot", Description: "Helps with tasks"},
						{AgentKey: "coder", DisplayName: "Coder", Description: "Writes code"},
					},
				},
			},
			wantContain: "## Delegation Targets",
		},
		{
			name: "orchestration enabled but no targets",
			cfg: PromptConfig{
				Identity:      true,
				IdentityData:  IdentityData{AgentName: "test-agent"},
				Orchestration: true,
				OrchestrationData: OrchestrationSectionData{
					Mode: ModeDelegate,
				},
			},
			wantContain: "Delegation Targets",
			wantAbsent:  true,
		},
		{
			name: "orchestration disabled",
			cfg: PromptConfig{
				Identity:     true,
				IdentityData: IdentityData{AgentName: "test-agent"},
			},
			wantContain: "Delegation Targets",
			wantAbsent:  true,
		},
		{
			name: "spawn mode skips section",
			cfg: PromptConfig{
				Identity:      true,
				IdentityData:  IdentityData{AgentName: "test-agent"},
				Orchestration: true,
				OrchestrationData: OrchestrationSectionData{
					Mode:            ModeSpawn,
					DelegateTargets: []DelegateTargetEntry{{AgentKey: "x"}},
				},
			},
			wantContain: "Delegation Targets",
			wantAbsent:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := builder.Build(tt.cfg)
			if err != nil {
				t.Fatalf("Build error: %v", err)
			}
			found := strings.Contains(output, tt.wantContain)
			if tt.wantAbsent && found {
				t.Errorf("output should NOT contain %q", tt.wantContain)
			}
			if !tt.wantAbsent && !found {
				t.Errorf("output should contain %q", tt.wantContain)
			}
		})
	}
}

func TestBridgePromptBuilder_OrchestrationTargetContent(t *testing.T) {
	builder := NewBridgePromptBuilder()
	cfg := PromptConfig{
		Identity:      true,
		IdentityData:  IdentityData{AgentName: "lead"},
		Orchestration: true,
		OrchestrationData: OrchestrationSectionData{
			Mode: ModeDelegate,
			DelegateTargets: []DelegateTargetEntry{
				{AgentKey: "worker-1", DisplayName: "Worker One", Description: "Does work"},
			},
		},
	}
	output, err := builder.Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(output, "worker-1") {
		t.Error("output should contain agent key 'worker-1'")
	}
	if !strings.Contains(output, "Worker One") {
		t.Error("output should contain display name")
	}
	if !strings.Contains(output, "Does work") {
		t.Error("output should contain description")
	}
}
