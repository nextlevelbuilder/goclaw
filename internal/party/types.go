package party

// PersonaInfo holds runtime persona metadata loaded from agent DB.
type PersonaInfo struct {
	AgentKey        string             `json:"agent_key"`
	DisplayName     string             `json:"display_name"`
	Emoji           string             `json:"emoji"`
	MovieRef        string             `json:"movie_ref"`
	SpeakingStyle   string             `json:"speaking_style"`
	ExpertiseWeight map[string]float64 `json:"expertise_weight,omitempty"`
}

// PersonaThought is one persona's independent thinking output (Deep Mode Step 1).
type PersonaThought struct {
	PersonaKey string `json:"persona_key"`
	Emoji      string `json:"emoji"`
	Content    string `json:"content"`
}

// PersonaMessage is a persona's spoken message in a round.
type PersonaMessage struct {
	PersonaKey  string `json:"persona_key"`
	DisplayName string `json:"display_name"`
	Emoji       string `json:"emoji"`
	Content     string `json:"content"`
	Thinking    string `json:"thinking,omitempty"`
}

// RoundResult contains all persona messages for one round.
type RoundResult struct {
	Round    int              `json:"round"`
	Mode     string           `json:"mode"`
	Messages []PersonaMessage `json:"messages"`
}

// SummaryResult contains the party discussion summary.
type SummaryResult struct {
	Topic         string       `json:"topic"`
	Rounds        int          `json:"rounds"`
	Personas      []string     `json:"personas"`
	Agreements    []string     `json:"agreements"`
	Disagreements []string     `json:"disagreements"`
	Decisions     []string     `json:"decisions"`
	ActionItems   []ActionItem `json:"action_items"`
	Compliance    []string     `json:"compliance_notes,omitempty"`
	Markdown      string       `json:"markdown"`
}

// ActionItem is a follow-up task from the discussion.
type ActionItem struct {
	Action   string `json:"action"`
	Assignee string `json:"assignee"`
	Deadline string `json:"deadline,omitempty"`
	CPLink   string `json:"cp_link,omitempty"`
}

// PresetTeam defines a preset team composition.
type PresetTeam struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Personas    []string `json:"personas"`
	UseCase     string   `json:"use_case"`
	Facilitator string   `json:"facilitator"`
	Mandatory   []string `json:"mandatory,omitempty"`
}

// PresetTeams returns the 6 preset team compositions.
func PresetTeams() []PresetTeam {
	return []PresetTeam{
		{Key: "payment_feature", Name: "Payment Feature", Personas: []string{"tony-stark-persona", "neo-persona", "batman-persona", "judge-dredd-persona", "columbo-persona"}, UseCase: "Payment flows, settlement", Facilitator: "gandalf-persona"},
		{Key: "security_review", Name: "Security Review", Personas: []string{"batman-persona", "judge-dredd-persona", "neo-persona", "scotty-persona"}, UseCase: "Threat modeling, pre-CP3", Facilitator: "batman-persona"},
		{Key: "sprint_planning", Name: "Sprint Planning", Personas: []string{"tony-stark-persona", "sherlock-persona", "neo-persona", "gandalf-persona", "columbo-persona"}, UseCase: "Sprint kickoff, PRD review", Facilitator: "gandalf-persona"},
		{Key: "architecture_decision", Name: "Architecture Decision", Personas: []string{"neo-persona", "spock-persona", "scotty-persona", "batman-persona"}, UseCase: "ADR, tech stack eval", Facilitator: "morpheus-persona"},
		{Key: "ux_review", Name: "UX Review", Personas: []string{"edna-mode-persona", "tony-stark-persona", "spider-man-persona", "ethan-hunt-persona", "columbo-persona"}, UseCase: "Design review", Facilitator: "edna-mode-persona"},
		{Key: "incident_response", Name: "Incident Response", Personas: []string{"scotty-persona", "neo-persona", "batman-persona", "nick-fury-persona"}, UseCase: "Production incidents", Facilitator: "nick-fury-persona"},
	}
}
