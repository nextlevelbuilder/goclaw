package agent

// OrchestrationMode controls which inter-agent tools are available.
type OrchestrationMode string

const (
	// ModeSpawn: self-clone only. spawn tool available.
	ModeSpawn OrchestrationMode = "spawn"

	// ModeDelegate: agent links + spawn. delegate tool available.
	ModeDelegate OrchestrationMode = "delegate"

	// ModeTeam: full team tasks + delegate + spawn.
	ModeTeam OrchestrationMode = "team"
)

// OrchestrationSectionData for system prompt template.
type OrchestrationSectionData struct {
	Mode            OrchestrationMode
	DelegateTargets []DelegateTargetEntry
	TeamContext     *TeamSectionData // only if ModeTeam
}

// DelegateTargetEntry is a single delegate target for prompt injection.
type DelegateTargetEntry struct {
	AgentKey    string
	DisplayName string
	Description string
}
