package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// baseNoopTeamStore — noop implementations for all TeamStore methods
// ============================================================

type baseNoopTeamStore struct{}

// TeamCRUDStore
func (b *baseNoopTeamStore) CreateTeam(_ context.Context, _ *store.TeamData) error {
	return fmt.Errorf("not implemented: CreateTeam")
}
func (b *baseNoopTeamStore) GetTeam(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return nil, fmt.Errorf("not implemented: GetTeam")
}
func (b *baseNoopTeamStore) GetTeamUnscoped(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return nil, fmt.Errorf("not implemented: GetTeamUnscoped")
}
func (b *baseNoopTeamStore) UpdateTeam(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return fmt.Errorf("not implemented: UpdateTeam")
}
func (b *baseNoopTeamStore) DeleteTeam(_ context.Context, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: DeleteTeam")
}
func (b *baseNoopTeamStore) ListTeams(_ context.Context) ([]store.TeamData, error) {
	return nil, fmt.Errorf("not implemented: ListTeams")
}
func (b *baseNoopTeamStore) AddMember(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: AddMember")
}
func (b *baseNoopTeamStore) RemoveMember(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: RemoveMember")
}
func (b *baseNoopTeamStore) ListMembers(_ context.Context, _ uuid.UUID) ([]store.TeamMemberData, error) {
	return nil, fmt.Errorf("not implemented: ListMembers")
}
func (b *baseNoopTeamStore) ListIdleMembers(_ context.Context, _ uuid.UUID) ([]store.TeamMemberData, error) {
	return nil, fmt.Errorf("not implemented: ListIdleMembers")
}
func (b *baseNoopTeamStore) GetTeamForAgent(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return nil, fmt.Errorf("not implemented: GetTeamForAgent")
}
func (b *baseNoopTeamStore) KnownUserIDs(_ context.Context, _ uuid.UUID, _ int) ([]string, error) {
	return nil, fmt.Errorf("not implemented: KnownUserIDs")
}
func (b *baseNoopTeamStore) ListTaskScopes(_ context.Context, _ uuid.UUID) ([]store.ScopeEntry, error) {
	return nil, fmt.Errorf("not implemented: ListTaskScopes")
}

// TaskStore
func (b *baseNoopTeamStore) CreateTask(_ context.Context, _ *store.TeamTaskData) error {
	return fmt.Errorf("not implemented: CreateTask")
}
func (b *baseNoopTeamStore) UpdateTask(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return fmt.Errorf("not implemented: UpdateTask")
}
func (b *baseNoopTeamStore) ListTasks(_ context.Context, _ uuid.UUID, _, _, _, _, _ string, _, _ int) ([]store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: ListTasks")
}
func (b *baseNoopTeamStore) GetTask(_ context.Context, _ uuid.UUID) (*store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: GetTask")
}
func (b *baseNoopTeamStore) GetTasksByIDs(_ context.Context, _ []uuid.UUID) ([]store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: GetTasksByIDs")
}
func (b *baseNoopTeamStore) SearchTasks(_ context.Context, _ uuid.UUID, _ string, _ int, _ string) ([]store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: SearchTasks")
}
func (b *baseNoopTeamStore) DeleteTask(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: DeleteTask")
}
func (b *baseNoopTeamStore) DeleteTasks(_ context.Context, _ []uuid.UUID, _ uuid.UUID) ([]uuid.UUID, error) {
	return nil, fmt.Errorf("not implemented: DeleteTasks")
}
func (b *baseNoopTeamStore) ClaimTask(_ context.Context, _, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: ClaimTask")
}
func (b *baseNoopTeamStore) AssignTask(_ context.Context, _, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: AssignTask")
}
func (b *baseNoopTeamStore) CompleteTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: CompleteTask")
}
func (b *baseNoopTeamStore) CancelTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: CancelTask")
}
func (b *baseNoopTeamStore) FailTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: FailTask")
}
func (b *baseNoopTeamStore) FailPendingTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: FailPendingTask")
}
func (b *baseNoopTeamStore) ReviewTask(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: ReviewTask")
}
func (b *baseNoopTeamStore) ApproveTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: ApproveTask")
}
func (b *baseNoopTeamStore) RejectTask(_ context.Context, _, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: RejectTask")
}
func (b *baseNoopTeamStore) UpdateTaskProgress(_ context.Context, _, _ uuid.UUID, _ int, _ string) error {
	return fmt.Errorf("not implemented: UpdateTaskProgress")
}
func (b *baseNoopTeamStore) RenewTaskLock(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: RenewTaskLock")
}
func (b *baseNoopTeamStore) ResetTaskStatus(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: ResetTaskStatus")
}
func (b *baseNoopTeamStore) ListActiveTasksByChatID(_ context.Context, _ string) ([]store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: ListActiveTasksByChatID")
}

// TaskCommentStore
func (b *baseNoopTeamStore) AddTaskComment(_ context.Context, _ *store.TeamTaskCommentData) error {
	return fmt.Errorf("not implemented: AddTaskComment")
}
func (b *baseNoopTeamStore) ListTaskComments(_ context.Context, _ uuid.UUID) ([]store.TeamTaskCommentData, error) {
	return nil, fmt.Errorf("not implemented: ListTaskComments")
}
func (b *baseNoopTeamStore) ListRecentTaskComments(_ context.Context, _ uuid.UUID, _ int) ([]store.TeamTaskCommentData, error) {
	return nil, fmt.Errorf("not implemented: ListRecentTaskComments")
}
func (b *baseNoopTeamStore) RecordTaskEvent(_ context.Context, _ *store.TeamTaskEventData) error {
	return fmt.Errorf("not implemented: RecordTaskEvent")
}
func (b *baseNoopTeamStore) ListTaskEvents(_ context.Context, _ uuid.UUID) ([]store.TeamTaskEventData, error) {
	return nil, fmt.Errorf("not implemented: ListTaskEvents")
}
func (b *baseNoopTeamStore) ListTeamEvents(_ context.Context, _ uuid.UUID, _, _ int) ([]store.TeamTaskEventData, error) {
	return nil, fmt.Errorf("not implemented: ListTeamEvents")
}
func (b *baseNoopTeamStore) AttachFileToTask(_ context.Context, _ *store.TeamTaskAttachmentData) error {
	return fmt.Errorf("not implemented: AttachFileToTask")
}
func (b *baseNoopTeamStore) GetAttachment(_ context.Context, _ uuid.UUID) (*store.TeamTaskAttachmentData, error) {
	return nil, fmt.Errorf("not implemented: GetAttachment")
}
func (b *baseNoopTeamStore) ListTaskAttachments(_ context.Context, _ uuid.UUID) ([]store.TeamTaskAttachmentData, error) {
	return nil, fmt.Errorf("not implemented: ListTaskAttachments")
}
func (b *baseNoopTeamStore) DetachFileFromTask(_ context.Context, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: DetachFileFromTask")
}
func (b *baseNoopTeamStore) BatchGetTaskSiblingsByBasenames(_ context.Context, _ uuid.UUID, _ []string, _ int) (map[string][]store.TaskSibling, error) {
	return nil, nil
}

// TaskRecoveryStore
func (b *baseNoopTeamStore) RecoverAllStaleTasks(_ context.Context) ([]store.RecoveredTaskInfo, error) {
	return nil, fmt.Errorf("not implemented: RecoverAllStaleTasks")
}
func (b *baseNoopTeamStore) ForceRecoverAllTasks(_ context.Context) ([]store.RecoveredTaskInfo, error) {
	return nil, fmt.Errorf("not implemented: ForceRecoverAllTasks")
}
func (b *baseNoopTeamStore) ListRecoverableTasks(_ context.Context, _ uuid.UUID) ([]store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: ListRecoverableTasks")
}
func (b *baseNoopTeamStore) MarkAllStaleTasks(_ context.Context, _ time.Time) ([]store.RecoveredTaskInfo, error) {
	return nil, fmt.Errorf("not implemented: MarkAllStaleTasks")
}
func (b *baseNoopTeamStore) MarkInReviewStaleTasks(_ context.Context, _ time.Time) ([]store.RecoveredTaskInfo, error) {
	return nil, fmt.Errorf("not implemented: MarkInReviewStaleTasks")
}
func (b *baseNoopTeamStore) FixOrphanedBlockedTasks(_ context.Context) ([]store.RecoveredTaskInfo, error) {
	return nil, fmt.Errorf("not implemented: FixOrphanedBlockedTasks")
}

// TaskFollowupStore
func (b *baseNoopTeamStore) SetTaskFollowup(_ context.Context, _, _ uuid.UUID, _ time.Time, _ int, _, _, _ string) error {
	return fmt.Errorf("not implemented: SetTaskFollowup")
}
func (b *baseNoopTeamStore) ClearTaskFollowup(_ context.Context, _ uuid.UUID) error {
	return fmt.Errorf("not implemented: ClearTaskFollowup")
}
func (b *baseNoopTeamStore) ListAllFollowupDueTasks(_ context.Context) ([]store.TeamTaskData, error) {
	return nil, fmt.Errorf("not implemented: ListAllFollowupDueTasks")
}
func (b *baseNoopTeamStore) IncrementFollowupCount(_ context.Context, _ uuid.UUID, _ *time.Time) error {
	return fmt.Errorf("not implemented: IncrementFollowupCount")
}
func (b *baseNoopTeamStore) ClearFollowupByScope(_ context.Context, _, _ string) (int, error) {
	return 0, fmt.Errorf("not implemented: ClearFollowupByScope")
}
func (b *baseNoopTeamStore) SetFollowupForActiveTasks(_ context.Context, _ uuid.UUID, _, _ string, _ time.Time, _ int, _ string) (int, error) {
	return 0, fmt.Errorf("not implemented: SetFollowupForActiveTasks")
}
func (b *baseNoopTeamStore) HasActiveMemberTasks(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, error) {
	return false, fmt.Errorf("not implemented: HasActiveMemberTasks")
}

// TeamAccessStore
func (b *baseNoopTeamStore) GrantTeamAccess(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return fmt.Errorf("not implemented: GrantTeamAccess")
}
func (b *baseNoopTeamStore) RevokeTeamAccess(_ context.Context, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented: RevokeTeamAccess")
}
func (b *baseNoopTeamStore) ListTeamGrants(_ context.Context, _ uuid.UUID) ([]store.TeamUserGrant, error) {
	return nil, fmt.Errorf("not implemented: ListTeamGrants")
}
func (b *baseNoopTeamStore) ListUserTeams(_ context.Context, _ string) ([]store.TeamData, error) {
	return nil, fmt.Errorf("not implemented: ListUserTeams")
}
func (b *baseNoopTeamStore) HasTeamAccess(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return false, fmt.Errorf("not implemented: HasTeamAccess")
}

// ============================================================
// mockTaskStore — in-memory TeamStore for unit tests
// ============================================================

type mockTaskStore struct {
	baseNoopTeamStore
	tasks       map[uuid.UUID]*store.TeamTaskData
	comments    map[uuid.UUID][]store.TeamTaskCommentData
	events      map[uuid.UUID][]store.TeamTaskEventData
	attachments map[uuid.UUID][]store.TeamTaskAttachmentData
	team        *store.TeamData
	members     []store.TeamMemberData
	taskSeq     int
	mu          sync.Mutex
}

func newMockTaskStore(team *store.TeamData, members []store.TeamMemberData) *mockTaskStore {
	return &mockTaskStore{
		tasks:       make(map[uuid.UUID]*store.TeamTaskData),
		comments:    make(map[uuid.UUID][]store.TeamTaskCommentData),
		events:      make(map[uuid.UUID][]store.TeamTaskEventData),
		attachments: make(map[uuid.UUID][]store.TeamTaskAttachmentData),
		team:        team,
		members:     members,
	}
}

func (s *mockTaskStore) GetTeam(_ context.Context, teamID uuid.UUID) (*store.TeamData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.team != nil && s.team.ID == teamID {
		return s.team, nil
	}
	return nil, fmt.Errorf("team not found: %s", teamID)
}

func (s *mockTaskStore) GetTeamForAgent(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.team, nil
}

func (s *mockTaskStore) ListMembers(_ context.Context, _ uuid.UUID) ([]store.TeamMemberData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.members, nil
}

func (s *mockTaskStore) GetTask(_ context.Context, taskID uuid.UUID) (*store.TeamTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, store.ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (s *mockTaskStore) GetTasksByIDs(_ context.Context, ids []uuid.UUID) ([]store.TeamTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.TeamTaskData, 0, len(ids))
	for _, id := range ids {
		if t, ok := s.tasks[id]; ok {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (s *mockTaskStore) CreateTask(_ context.Context, task *store.TeamTaskData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	task.ID = uuid.New()
	s.taskSeq++
	task.TaskNumber = s.taskSeq
	task.CreatedAt = now
	task.UpdatedAt = now
	if task.Identifier == "" {
		task.Identifier = fmt.Sprintf("T-%d", task.TaskNumber)
	}
	cp := *task
	s.tasks[task.ID] = &cp
	return nil
}

func (s *mockTaskStore) UpdateTask(_ context.Context, taskID uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	for k, v := range updates {
		switch k {
		case "status":
			if sv, ok := v.(string); ok {
				t.Status = sv
			}
		case "result":
			if sv, ok := v.(string); ok {
				t.Result = &sv
			}
		case "metadata":
			if mv, ok := v.(map[string]any); ok {
				t.Metadata = mv
			}
		case "description":
			if sv, ok := v.(string); ok {
				t.Description = sv
			}
		case "subject":
			if sv, ok := v.(string); ok {
				t.Subject = sv
			}
		case "blocked_by":
			if ids, ok := v.([]uuid.UUID); ok {
				t.BlockedBy = ids
			}
		case "owner_agent_id":
			if id, ok := v.(uuid.UUID); ok {
				t.OwnerAgentID = &id
			}
		case "locked_at":
			if tv, ok := v.(time.Time); ok {
				t.LockedAt = &tv
			}
		case "lock_expires_at":
			if tv, ok := v.(time.Time); ok {
				t.LockExpiresAt = &tv
			}
		case "progress_percent":
			if iv, ok := v.(int); ok {
				t.ProgressPercent = iv
			}
		case "progress_step":
			if sv, ok := v.(string); ok {
				t.ProgressStep = sv
			}
		case "followup_at":
			if tv, ok := v.(time.Time); ok {
				t.FollowupAt = &tv
			}
		case "followup_count":
			if iv, ok := v.(int); ok {
				t.FollowupCount = iv
			}
		case "followup_max":
			if iv, ok := v.(int); ok {
				t.FollowupMax = iv
			}
		case "followup_message":
			if sv, ok := v.(string); ok {
				t.FollowupMessage = sv
			}
		case "followup_channel":
			if sv, ok := v.(string); ok {
				t.FollowupChannel = sv
			}
		case "followup_chat_id":
			if sv, ok := v.(string); ok {
				t.FollowupChatID = sv
			}
		}
	}
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ClaimTask(_ context.Context, taskID, agentID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusPending {
		return fmt.Errorf("cannot claim task with status %q", t.Status)
	}
	if t.OwnerAgentID != nil && *t.OwnerAgentID != agentID {
		return fmt.Errorf("task already owned by another agent")
	}
	now := time.Now()
	t.Status = store.TeamTaskStatusInProgress
	t.OwnerAgentID = &agentID
	t.LockedAt = &now
	t.UpdatedAt = now
	return nil
}

func (s *mockTaskStore) AssignTask(_ context.Context, taskID, agentID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusPending {
		return fmt.Errorf("cannot assign task with status %q", t.Status)
	}
	now := time.Now()
	t.Status = store.TeamTaskStatusInProgress
	t.OwnerAgentID = &agentID
	t.LockedAt = &now
	t.UpdatedAt = now
	return nil
}

func (s *mockTaskStore) CompleteTask(_ context.Context, taskID, _ uuid.UUID, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("cannot complete task with status %q", t.Status)
	}
	t.Status = store.TeamTaskStatusCompleted
	t.Result = &result
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) CancelTask(_ context.Context, taskID, _ uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status == store.TeamTaskStatusCompleted || t.Status == store.TeamTaskStatusCancelled {
		return fmt.Errorf("cannot cancel task with status %q", t.Status)
	}
	t.Status = store.TeamTaskStatusCancelled
	t.Result = &reason
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) FailTask(_ context.Context, taskID, _ uuid.UUID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("cannot fail task with status %q", t.Status)
	}
	result := "FAILED: " + errMsg
	t.Status = store.TeamTaskStatusFailed
	t.Result = &result
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) FailPendingTask(_ context.Context, taskID, _ uuid.UUID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusPending && t.Status != store.TeamTaskStatusBlocked {
		return fmt.Errorf("cannot fail-pending task with status %q", t.Status)
	}
	t.Status = store.TeamTaskStatusFailed
	t.Result = &errMsg
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ReviewTask(_ context.Context, taskID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("cannot review task with status %q", t.Status)
	}
	t.Status = store.TeamTaskStatusInReview
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ApproveTask(_ context.Context, taskID, _ uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusInReview {
		return fmt.Errorf("cannot approve task with status %q", t.Status)
	}
	t.Status = store.TeamTaskStatusCompleted
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) RejectTask(_ context.Context, taskID, _ uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusInReview {
		return fmt.Errorf("cannot reject task with status %q", t.Status)
	}
	t.Status = store.TeamTaskStatusCancelled
	t.Result = &reason
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ResetTaskStatus(_ context.Context, taskID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	t.Status = store.TeamTaskStatusPending
	t.LockedAt = nil
	t.LockExpiresAt = nil
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) UpdateTaskProgress(_ context.Context, taskID, _ uuid.UUID, percent int, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	if t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("cannot update progress for task with status %q", t.Status)
	}
	t.ProgressPercent = percent
	t.ProgressStep = step
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) RenewTaskLock(_ context.Context, taskID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	expires := time.Now().Add(10 * time.Minute)
	t.LockExpiresAt = &expires
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ListTasks(_ context.Context, teamID uuid.UUID, _, _, _, _, _ string, _, _ int) ([]store.TeamTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.TeamTaskData
	for _, t := range s.tasks {
		if t.TeamID == teamID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (s *mockTaskStore) SearchTasks(_ context.Context, teamID uuid.UUID, query string, limit int, _ string) ([]store.TeamTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(query)
	var out []store.TeamTaskData
	for _, t := range s.tasks {
		if t.TeamID == teamID && strings.Contains(strings.ToLower(t.Subject), q) {
			out = append(out, *t)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *mockTaskStore) AddTaskComment(_ context.Context, comment *store.TeamTaskCommentData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	comment.ID = uuid.New()
	comment.CreatedAt = time.Now()
	cp := *comment
	s.comments[comment.TaskID] = append(s.comments[comment.TaskID], cp)
	return nil
}

func (s *mockTaskStore) ListTaskComments(_ context.Context, taskID uuid.UUID) ([]store.TeamTaskCommentData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.comments[taskID], nil
}

func (s *mockTaskStore) ListRecentTaskComments(_ context.Context, taskID uuid.UUID, limit int) ([]store.TeamTaskCommentData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.comments[taskID]
	if limit <= 0 || len(all) <= limit {
		return all, nil
	}
	return all[len(all)-limit:], nil
}

func (s *mockTaskStore) ListTaskEvents(_ context.Context, taskID uuid.UUID) ([]store.TeamTaskEventData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[taskID], nil
}

func (s *mockTaskStore) AttachFileToTask(_ context.Context, att *store.TeamTaskAttachmentData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	att.ID = uuid.New()
	att.CreatedAt = time.Now()
	cp := *att
	s.attachments[att.TaskID] = append(s.attachments[att.TaskID], cp)
	return nil
}

func (s *mockTaskStore) ListTaskAttachments(_ context.Context, taskID uuid.UUID) ([]store.TeamTaskAttachmentData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attachments[taskID], nil
}

func (s *mockTaskStore) SetTaskFollowup(_ context.Context, taskID, _ uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	t.FollowupAt = &followupAt
	t.FollowupMax = max
	t.FollowupMessage = message
	t.FollowupChannel = channel
	t.FollowupChatID = chatID
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ClearTaskFollowup(_ context.Context, taskID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return store.ErrTaskNotFound
	}
	t.FollowupAt = nil
	t.FollowupCount = 0
	t.FollowupMax = 0
	t.FollowupMessage = ""
	t.FollowupChannel = ""
	t.FollowupChatID = ""
	t.UpdatedAt = time.Now()
	return nil
}

func (s *mockTaskStore) ListRecoverableTasks(_ context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.TeamTaskData
	for _, t := range s.tasks {
		if t.TeamID != teamID {
			continue
		}
		if t.Status == store.TeamTaskStatusPending || t.Status == store.TeamTaskStatusStale {
			out = append(out, *t)
		}
	}
	return out, nil
}

// ============================================================
// capturedEvent / capturedDispatch
// ============================================================

type capturedEvent struct {
	Name    string
	Payload any
}

type capturedDispatch struct {
	TaskID  uuid.UUID
	AgentID uuid.UUID
}

// ============================================================
// mockBackend — implements TeamToolBackend for unit tests
// ============================================================

type mockBackend struct {
	team       *store.TeamData
	members    []store.TeamMemberData
	agents     map[uuid.UUID]*store.AgentData
	agentKeys  map[string]*store.AgentData
	taskStore  *mockTaskStore
	events     []capturedEvent
	dispatches []capturedDispatch
	inbound    []bus.InboundMessage
	mu         sync.Mutex
}

func newMockBackend(team *store.TeamData, members []store.TeamMemberData, agents []*store.AgentData) *mockBackend {
	mb := &mockBackend{
		team:      team,
		members:   members,
		agents:    make(map[uuid.UUID]*store.AgentData),
		agentKeys: make(map[string]*store.AgentData),
		taskStore: newMockTaskStore(team, members),
	}
	for _, ag := range agents {
		mb.agents[ag.ID] = ag
		mb.agentKeys[ag.AgentKey] = ag
	}
	return mb
}

func (mb *mockBackend) ResolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error) {
	if mb.team == nil {
		return nil, uuid.Nil, fmt.Errorf("no team configured in mock")
	}
	agentID := store.AgentIDFromContext(ctx)
	return mb.team, agentID, nil
}

func (mb *mockBackend) RequireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error {
	channel := ToolChannelFromCtx(ctx)
	if channel == ChannelTeammate || channel == ChannelSystem {
		return nil
	}
	if agentID != team.LeadAgentID {
		return fmt.Errorf("only the team lead can perform this action")
	}
	return nil
}

func (mb *mockBackend) Store() store.TeamStore { return mb.taskStore }

func (mb *mockBackend) ResolveAgentByKey(_ context.Context, key string) (uuid.UUID, error) {
	if ag, ok := mb.agentKeys[key]; ok {
		return ag.ID, nil
	}
	return uuid.Nil, fmt.Errorf("agent not found: %s", key)
}

func (mb *mockBackend) AgentKeyFromID(_ context.Context, id uuid.UUID) string {
	if ag, ok := mb.agents[id]; ok {
		return ag.AgentKey
	}
	return id.String()
}

func (mb *mockBackend) AgentDisplayName(_ context.Context, key string) string {
	if ag, ok := mb.agentKeys[key]; ok {
		return ag.DisplayName
	}
	return ""
}

func (mb *mockBackend) CachedListMembers(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]store.TeamMemberData, error) {
	return mb.members, nil
}

func (mb *mockBackend) CachedGetAgentByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if ag, ok := mb.agents[id]; ok {
		return ag, nil
	}
	return nil, fmt.Errorf("agent not found: %s", id)
}

func (mb *mockBackend) PreWarmAgentKeyCache(_ context.Context, _ []string)    {}
func (mb *mockBackend) PreWarmAgentIDCache(_ context.Context, _ []uuid.UUID)  {}

func (mb *mockBackend) BroadcastTeamEvent(_ context.Context, name string, payload any) {
	mb.mu.Lock()
	mb.events = append(mb.events, capturedEvent{Name: name, Payload: payload})
	mb.mu.Unlock()
}

func (mb *mockBackend) DispatchTaskToAgent(_ context.Context, task *store.TeamTaskData, _ *store.TeamData, agentID uuid.UUID) {
	mb.mu.Lock()
	mb.dispatches = append(mb.dispatches, capturedDispatch{TaskID: task.ID, AgentID: agentID})
	mb.mu.Unlock()
}

func (mb *mockBackend) TryPublishInbound(msg bus.InboundMessage) bool {
	mb.mu.Lock()
	mb.inbound = append(mb.inbound, msg)
	mb.mu.Unlock()
	return true
}

func (mb *mockBackend) BuildBlockerResultsSummary(_ context.Context, _ *store.TeamTaskData) string { return "" }
func (mb *mockBackend) BuildRecentCommentsSummary(_ context.Context, _ uuid.UUID) string           { return "" }
func (mb *mockBackend) RestoreTraceContext(ctx context.Context, _ *store.TeamTaskData) context.Context {
	return ctx
}
func (mb *mockBackend) FollowupDelayMinutes(_ *store.TeamData) int  { return 30 }
func (mb *mockBackend) FollowupMaxReminders(_ *store.TeamData) int  { return 0 }
func (mb *mockBackend) DataDir() string                             { return "/tmp/test" }

// ============================================================
// newTestTeamSetup — standard test fixture
// ============================================================

var (
	testTeamID   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	testLeadID   = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	testMemberID = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	testMember2ID = uuid.MustParse("00000000-0000-0000-0000-000000000004")
	testTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000099")
)

func newTestTeamSetup() (*mockBackend, *TeamTasksTool, uuid.UUID, uuid.UUID, context.Context) {
	team := &store.TeamData{
		BaseModel:   store.BaseModel{ID: testTeamID},
		Name:        "test-team",
		LeadAgentID: testLeadID,
		Status:      store.TeamStatusActive,
	}

	leadAgent := &store.AgentData{
		BaseModel:   store.BaseModel{ID: testLeadID},
		AgentKey:    "lead-agent",
		DisplayName: "Lead Agent",
	}
	memberAgent := &store.AgentData{
		BaseModel:   store.BaseModel{ID: testMemberID},
		AgentKey:    "member-agent",
		DisplayName: "Member Agent",
	}
	member2Agent := &store.AgentData{
		BaseModel:   store.BaseModel{ID: testMember2ID},
		AgentKey:    "member2-agent",
		DisplayName: "Member 2 Agent",
	}

	members := []store.TeamMemberData{
		{TeamID: testTeamID, AgentID: testLeadID, Role: store.TeamRoleLead, AgentKey: "lead-agent"},
		{TeamID: testTeamID, AgentID: testMemberID, Role: store.TeamRoleMember, AgentKey: "member-agent"},
		{TeamID: testTeamID, AgentID: testMember2ID, Role: store.TeamRoleMember, AgentKey: "member2-agent"},
	}

	mb := newMockBackend(team, members, []*store.AgentData{leadAgent, memberAgent, member2Agent})
	tool := NewTeamTasksTool(mb, FullTeamPolicy{})

	ctx := context.Background()
	ctx = store.WithTenantID(ctx, testTenantID)
	ctx = store.WithAgentID(ctx, testLeadID)
	ctx = WithToolChannel(ctx, ChannelDashboard)
	ctx = WithToolChatID(ctx, testTeamID.String())
	ctx = WithTaskActionFlags(ctx, &TaskActionFlags{})

	return mb, tool, testLeadID, testMemberID, ctx
}
