package tools

import (
	"context"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TaskEventOption customizes optional fields on a TeamTaskEventPayload.
type TaskEventOption func(*protocol.TeamTaskEventPayload)

// BuildTaskEventPayload constructs a TeamTaskEventPayload with required fields
// (teamID, taskID, status, actor) and a UTC timestamp. Optional fields are set
// via TaskEventOption functions.
func BuildTaskEventPayload(
	teamID, taskID string,
	status string,
	actorType, actorID string,
	opts ...TaskEventOption,
) protocol.TeamTaskEventPayload {
	p := protocol.TeamTaskEventPayload{
		TeamID:    teamID,
		TaskID:    taskID,
		Status:    status,
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: actorType,
		ActorID:   actorID,
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// WithTaskNumber sets TaskNumber on the payload.
func WithTaskNumber(n int) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.TaskNumber = n }
}

// WithSubject sets Subject on the payload.
func WithSubject(s string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.Subject = s }
}

// WithTaskInfo sets both TaskNumber and Subject.
func WithTaskInfo(taskNumber int, subject string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) {
		p.TaskNumber = taskNumber
		p.Subject = subject
	}
}

// WithOwnerAgentKey sets OwnerAgentKey on the payload.
func WithOwnerAgentKey(k string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.OwnerAgentKey = k }
}

// WithOwnerDisplayName sets OwnerDisplayName on the payload.
func WithOwnerDisplayName(n string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.OwnerDisplayName = n }
}

// WithOwner sets both OwnerAgentKey and OwnerDisplayName.
func WithOwner(agentKey, displayName string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) {
		p.OwnerAgentKey = agentKey
		p.OwnerDisplayName = displayName
	}
}

// WithReason sets Reason on the payload.
func WithReason(r string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.Reason = r }
}

// WithUserID sets UserID on the payload.
func WithUserID(id string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.UserID = id }
}

// WithChannel sets Channel on the payload.
func WithChannel(ch string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.Channel = ch }
}

// WithChatID sets ChatID on the payload.
func WithChatID(id string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.ChatID = id }
}

// WithPeerKind sets PeerKind on the payload for correct session routing (#266).
func WithPeerKind(pk string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.PeerKind = pk }
}

// WithLocalKey sets LocalKey on the payload for forum topic routing.
func WithLocalKey(lk string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.LocalKey = lk }
}

// WithCommentText sets CommentText on the payload.
func WithCommentText(t string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.CommentText = t }
}

// WithProgress sets ProgressPercent and ProgressStep on the payload.
func WithProgress(percent int, step string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) {
		p.ProgressPercent = percent
		p.ProgressStep = step
	}
}

// WithContextInfo extracts UserID, Channel, ChatID, and PeerKind from the context
// using standard tool context accessors.
func WithContextInfo(ctx context.Context) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) {
		p.UserID = store.UserIDFromContext(ctx)
		p.Channel = ToolChannelFromCtx(ctx)
		p.ChatID = ToolChatIDFromCtx(ctx)
		p.PeerKind = ToolPeerKindFromCtx(ctx)
		p.LocalKey = ToolLocalKeyFromCtx(ctx)
	}
}

// WithTimestamp overrides the auto-generated UTC timestamp. Use only when
// the event timestamp must differ from time.Now() (e.g. task creation time).
func WithTimestamp(ts string) TaskEventOption {
	return func(p *protocol.TeamTaskEventPayload) { p.Timestamp = ts }
}

// TeamTaskEventOptions carries the non-authoritative, caller-supplied fields for
// BuildTeamTaskEventPayload. Everything that identifies the task or reflects its
// committed state (team ID, task ID/number, subject, status, workflow ID/step,
// plan revision, owner agent key) is derived from the task row, never from here.
type TeamTaskEventOptions struct {
	Reason, UserID, Channel, ChatID, PeerKind, LocalKey, CommentText string
	ProgressPercent                                                  int
	ProgressStep                                                     string
	ActorType, ActorID                                               string
}

// BuildTeamTaskEventPayload constructs a TeamTaskEventPayload whose identity,
// committed status, workflow linkage, plan revision, and owner agent key ALWAYS
// come from the authoritative committed task row. Only routing/actor/reason/
// progress context is taken from opts. The owner display name is supplied by the
// caller (resolved via the authoritative agent lookup) so a stale request value
// or a raw UUID can never land in the payload.
//
// OwnerAgentKey is overwritten from the row unconditionally — including when the
// row has no owner — so an unassign clears any stale caller-supplied key. The
// builder never queries the DB itself.
func BuildTeamTaskEventPayload(task *store.TeamTaskData, ownerDisplayName string, opts TeamTaskEventOptions) protocol.TeamTaskEventPayload {
	p := protocol.TeamTaskEventPayload{
		Timestamp:       time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Reason:          opts.Reason,
		UserID:          opts.UserID,
		Channel:         opts.Channel,
		ChatID:          opts.ChatID,
		PeerKind:        opts.PeerKind,
		LocalKey:        opts.LocalKey,
		CommentText:     opts.CommentText,
		ProgressPercent: opts.ProgressPercent,
		ProgressStep:    opts.ProgressStep,
		ActorType:       opts.ActorType,
		ActorID:         opts.ActorID,
	}
	applyAuthoritativeTaskIdentity(&p, task)
	p.OwnerDisplayName = ""
	if task.OwnerAgentKey != "" {
		p.OwnerDisplayName = ownerDisplayName
	}
	return p
}

// applyAuthoritativeTaskIdentity overwrites every identity/status/workflow/owner
// field on the payload from the committed task row. OwnerAgentKey is written
// unconditionally (including empty) so a stale caller value never survives.
func applyAuthoritativeTaskIdentity(p *protocol.TeamTaskEventPayload, task *store.TeamTaskData) {
	if p == nil || task == nil {
		return
	}
	p.TeamID = task.TeamID.String()
	p.TaskID = task.ID.String()
	p.TaskNumber = task.TaskNumber
	p.Subject = task.Subject
	p.Status = task.Status
	p.PlanRevision = task.PlanRevision
	p.WorkflowStepID = task.WorkflowStepID
	if task.WorkflowID != nil {
		p.WorkflowID = task.WorkflowID.String()
	} else {
		p.WorkflowID = ""
	}
	p.OwnerAgentKey = task.OwnerAgentKey
}
