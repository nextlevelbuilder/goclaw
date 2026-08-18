package protocol

// RunPausedPayload and RunWokenPayload describe intentional suspend/wake
// (runs.pause / runs.wake). They ride the agent event stream as typed payloads
// of the AgentEventRunPaused / AgentEventRunWoken subtypes (see events.go).
// Iteration and CheckpointSeq identify where the run was checkpointed: Iteration
// is the saved pipeline iteration (0 when no durable checkpoint existed yet),
// and CheckpointSeq is 0 for the agent_runs.checkpoint payload (snapshot seq is
// owned by the run_checkpoint_snapshots table).
type RunPausedPayload struct {
	RunID         string `json:"run_id"`
	Iteration     int    `json:"iteration"`
	CheckpointSeq int    `json:"checkpoint_seq"`
}

// RunWokenPayload describes a wake from suspension (runs.wake resumed the run
// through the shared resume path).
type RunWokenPayload struct {
	RunID         string `json:"run_id"`
	Iteration     int    `json:"iteration"`
	CheckpointSeq int    `json:"checkpoint_seq"`
}