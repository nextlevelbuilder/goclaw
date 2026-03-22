package protocol

// Party Mode methods.
const (
	MethodPartyStart      = "party.start"
	MethodPartyRound      = "party.round"
	MethodPartyQuestion   = "party.question"
	MethodPartyAddContext = "party.add_context"
	MethodPartySummary    = "party.summary"
	MethodPartyExit       = "party.exit"
	MethodPartyList       = "party.list"
)

// Party Mode events.
const (
	EventPartyStarted         = "party.started"
	EventPartyPersonaIntro    = "party.persona.intro"
	EventPartyRoundStarted    = "party.round.started"
	EventPartyPersonaThinking = "party.persona.thinking"
	EventPartyPersonaSpoke    = "party.persona.spoke"
	EventPartyRoundComplete   = "party.round.complete"
	EventPartyContextAdded    = "party.context.added"
	EventPartySummaryReady    = "party.summary.ready"
	EventPartyArtifact        = "party.artifact"
	EventPartyClosed          = "party.closed"
)
