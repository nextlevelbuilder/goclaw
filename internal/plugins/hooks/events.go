// Package hooks — Lifecycle hook event definitions (CP-08).
// 26 events covering the complete agent pipeline lifecycle.
package hooks

// Event identifies a lifecycle hook point.
type Event string

const (
	// Tool lifecycle
	PreToolUse      Event = "PreToolUse"
	PostToolUse     Event = "PostToolUse"
	PostToolFailure Event = "PostToolUseFailure"

	// Permission
	PermissionDenied  Event = "PermissionDenied"
	PermissionRequest Event = "PermissionRequest"

	// Session
	SessionStart Event = "SessionStart"
	SessionEnd   Event = "SessionEnd"
	Setup        Event = "Setup"

	// Agent lifecycle
	SubagentStart Event = "SubagentStart"
	SubagentStop  Event = "SubagentStop"
	TeammateIdle  Event = "TeammateIdle"

	// Task
	TaskCreated   Event = "TaskCreated"
	TaskCompleted Event = "TaskCompleted"
	Stop          Event = "Stop"
	StopFailure   Event = "StopFailure"

	// Context
	PreCompact  Event = "PreCompact"
	PostCompact Event = "PostCompact"
	Notification Event = "Notification"

	// Filesystem
	FileChanged    Event = "FileChanged"
	CwdChanged     Event = "CwdChanged"
	WorktreeCreate Event = "WorktreeCreate"
	WorktreeRemove Event = "WorktreeRemove"

	// Config
	ConfigChange       Event = "ConfigChange"
	InstructionsLoaded Event = "InstructionsLoaded"
	UserPromptSubmit   Event = "UserPromptSubmit"

	// Channel
	ChannelMessage Event = "ChannelMessage"
)

// AllEvents returns all defined hook events.
func AllEvents() []Event {
	return []Event{
		PreToolUse, PostToolUse, PostToolFailure,
		PermissionDenied, PermissionRequest,
		SessionStart, SessionEnd, Setup,
		SubagentStart, SubagentStop, TeammateIdle,
		TaskCreated, TaskCompleted, Stop, StopFailure,
		PreCompact, PostCompact, Notification,
		FileChanged, CwdChanged, WorktreeCreate, WorktreeRemove,
		ConfigChange, InstructionsLoaded, UserPromptSubmit,
		ChannelMessage,
	}
}
