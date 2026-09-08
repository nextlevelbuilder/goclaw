package methods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Coverage for teams.tasks.cancel / teams.tasks.retry — the human-in-the-loop
// transitions the dashboard could not trigger before. The store is a stub that
// records calls; msgBus stays nil so no dispatch is attempted (the handlers
// guard on it, same as teams.tasks.assign).

// ---- stub team store ----

type humanTaskStore struct {
	store.TeamStore // nil: any method not overridden below panics loudly
	mu              sync.Mutex
	task            *store.TeamTaskData
	team            *store.TeamData
	getErr          error

	cancelled []string // reasons
	comments  []string
	resets    int
	assigned  []uuid.UUID
}

func (s *humanTaskStore) GetTask(_ context.Context, id uuid.UUID) (*store.TeamTaskData, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil || s.task.ID != id {
		return nil, store.ErrTaskNotFound
	}
	cp := *s.task
	return &cp, nil
}

func (s *humanTaskStore) GetTeam(_ context.Context, id uuid.UUID) (*store.TeamData, error) {
	if s.team == nil || s.team.ID != id {
		return nil, errors.New("team not found")
	}
	return s.team, nil
}

func (s *humanTaskStore) CancelTask(_ context.Context, _, _ uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = append(s.cancelled, reason)
	s.task.Status = store.TeamTaskStatusCancelled
	return nil
}

func (s *humanTaskStore) AddTaskComment(_ context.Context, c *store.TeamTaskCommentData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments = append(s.comments, c.Content)
	return nil
}

func (s *humanTaskStore) ResetTaskStatus(_ context.Context, _, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resets++
	s.task.Status = store.TeamTaskStatusPending
	return nil
}

func (s *humanTaskStore) AssignTask(_ context.Context, _, agentID, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.task.Status != store.TeamTaskStatusPending {
		return errors.New("assign requires pending")
	}
	s.assigned = append(s.assigned, agentID)
	s.task.Status = store.TeamTaskStatusInProgress
	s.task.OwnerAgentID = &agentID
	return nil
}

// ---- harness ----

type humanTaskFixture struct {
	m      *TeamsMethods
	st     *humanTaskStore
	teamID uuid.UUID
	taskID uuid.UUID
	leadID uuid.UUID
	member uuid.UUID
}

func newHumanTaskFixture(t *testing.T, status string) *humanTaskFixture {
	t.Helper()
	teamID, taskID := uuid.New(), uuid.New()
	leadID, memberID := uuid.New(), uuid.New()
	owner := memberID
	st := &humanTaskStore{
		task: &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    teamID, TaskNumber: 7, Subject: "Ship it",
			Status: status, OwnerAgentID: &owner, Channel: "telegram", ChatID: "42",
		},
		team: &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, LeadAgentID: leadID},
	}
	return &humanTaskFixture{
		m:      &TeamsMethods{teamStore: st},
		st:     st,
		teamID: teamID, taskID: taskID, leadID: leadID, member: memberID,
	}
}

// call runs a handler and decodes the single response frame it sends.
func (f *humanTaskFixture) call(t *testing.T, handler func(context.Context, *gateway.Client, *protocol.RequestFrame), params map[string]any) (ok bool, errCode string, errMsg string) {
	t.Helper()
	client, frames := gateway.NewCapturingTestClient(permissions.RoleAdmin, uuid.Nil, "human-1", 4)
	raw, _ := json.Marshal(params)
	handler(context.Background(), client, &protocol.RequestFrame{ID: "1", Params: raw})
	select {
	case b := <-frames:
		var resp struct {
			OK    bool `json:"ok"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			t.Fatalf("bad response frame: %v: %s", err, b)
		}
		if resp.Error != nil {
			return resp.OK, resp.Error.Code, resp.Error.Message
		}
		return resp.OK, "", ""
	default:
		t.Fatal("handler sent no response")
		return false, "", ""
	}
}

// ---- cancel ----

func TestTaskCancel_BlockedTaskCancelledWithReasonComment(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusBlocked)
	ok, code, msg := f.call(t, f.m.handleTaskCancel, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(), "reason": "wrong approach, dropping",
	})
	if !ok {
		t.Fatalf("expected ok, got %s: %s", code, msg)
	}
	if len(f.st.cancelled) != 1 || f.st.cancelled[0] != "wrong approach, dropping" {
		t.Fatalf("CancelTask calls = %v", f.st.cancelled)
	}
	if len(f.st.comments) != 1 || f.st.comments[0] != "wrong approach, dropping" {
		t.Fatalf("reason must be posted as a comment, got %v", f.st.comments)
	}
}

func TestTaskCancel_NoReasonUsesDefaultAndSkipsComment(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusStale)
	if ok, code, msg := f.call(t, f.m.handleTaskCancel, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(),
	}); !ok {
		t.Fatalf("expected ok, got %s: %s", code, msg)
	}
	if got := f.st.cancelled; len(got) != 1 || got[0] != "Cancelled by human" {
		t.Fatalf("default reason expected, got %v", got)
	}
	if len(f.st.comments) != 0 {
		t.Fatalf("no comment expected without a reason, got %v", f.st.comments)
	}
}

func TestTaskCancel_TerminalStatusRejected(t *testing.T) {
	for _, status := range []string{store.TeamTaskStatusCompleted, store.TeamTaskStatusCancelled} {
		f := newHumanTaskFixture(t, status)
		ok, code, _ := f.call(t, f.m.handleTaskCancel, map[string]any{
			"teamId": f.teamID.String(), "taskId": f.taskID.String(),
		})
		if ok || code != protocol.ErrInvalidRequest {
			t.Fatalf("%s: expected INVALID_REQUEST, got ok=%v code=%s", status, ok, code)
		}
		if len(f.st.cancelled) != 0 {
			t.Fatalf("%s: store must not be touched", status)
		}
	}
}

func TestTaskCancel_WrongTeamIsNotFound(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusInProgress)
	ok, code, _ := f.call(t, f.m.handleTaskCancel, map[string]any{
		"teamId": uuid.NewString(), "taskId": f.taskID.String(),
	})
	if ok || code != protocol.ErrNotFound {
		t.Fatalf("expected NOT_FOUND for foreign team, got ok=%v code=%s", ok, code)
	}
	if len(f.st.cancelled) != 0 {
		t.Fatal("IDOR: task of another team was cancelled")
	}
}

// ---- retry ----

func TestTaskRetry_RequiresComment(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusFailed)
	for _, comment := range []string{"", "   \n"} {
		ok, code, _ := f.call(t, f.m.handleTaskRetry, map[string]any{
			"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": comment,
		})
		if ok || code != protocol.ErrInvalidRequest {
			t.Fatalf("comment %q: expected INVALID_REQUEST, got ok=%v code=%s", comment, ok, code)
		}
	}
	if f.st.resets != 0 || len(f.st.assigned) != 0 || len(f.st.comments) != 0 {
		t.Fatal("store must not be touched when the comment is missing")
	}
}

func TestTaskRetry_FailedTaskGoesBackToOwnerWithComment(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusFailed)
	ok, code, msg := f.call(t, f.m.handleTaskRetry, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": "use the mirrored image, not docker hub",
	})
	if !ok {
		t.Fatalf("expected ok, got %s: %s", code, msg)
	}
	if len(f.st.comments) != 1 || !strings.Contains(f.st.comments[0], "mirrored image") {
		t.Fatalf("comment not stored: %v", f.st.comments)
	}
	if f.st.resets != 1 {
		t.Fatalf("ResetTaskStatus calls = %d", f.st.resets)
	}
	if len(f.st.assigned) != 1 || f.st.assigned[0] != f.member {
		t.Fatalf("task must go back to its owner, assigned = %v", f.st.assigned)
	}
	if f.st.task.Status != store.TeamTaskStatusInProgress {
		t.Fatalf("status = %s", f.st.task.Status)
	}
}

func TestTaskRetry_BlockedWithoutBlockersIsAllowed(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusBlocked)
	if ok, code, msg := f.call(t, f.m.handleTaskRetry, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": "answer: option B",
	}); !ok {
		t.Fatalf("expected ok, got %s: %s", code, msg)
	}
	if f.st.resets != 1 || len(f.st.assigned) != 1 {
		t.Fatalf("resets=%d assigned=%v", f.st.resets, f.st.assigned)
	}
}

func TestTaskRetry_BlockedByOtherTasksIsRejected(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusBlocked)
	f.st.task.BlockedBy = []uuid.UUID{uuid.New()}
	ok, code, msg := f.call(t, f.m.handleTaskRetry, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": "go",
	})
	if ok || code != protocol.ErrInvalidRequest || !strings.Contains(msg, "blocked by other tasks") {
		t.Fatalf("expected blocked-by rejection, got ok=%v code=%s msg=%s", ok, code, msg)
	}
	if f.st.resets != 0 || len(f.st.comments) != 0 {
		t.Fatal("store must not be touched")
	}
}

func TestTaskRetry_NonRetryableStatusRejected(t *testing.T) {
	for _, status := range []string{store.TeamTaskStatusInProgress, store.TeamTaskStatusCompleted, store.TeamTaskStatusPending} {
		f := newHumanTaskFixture(t, status)
		ok, code, _ := f.call(t, f.m.handleTaskRetry, map[string]any{
			"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": "go",
		})
		if ok || code != protocol.ErrInvalidRequest {
			t.Fatalf("%s: expected INVALID_REQUEST, got ok=%v code=%s", status, ok, code)
		}
	}
}

func TestTaskRetry_LeadOwnedTaskRejected(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusFailed)
	lead := f.leadID
	f.st.task.OwnerAgentID = &lead
	ok, code, msg := f.call(t, f.m.handleTaskRetry, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": "go",
	})
	if ok || code != protocol.ErrInvalidRequest || !strings.Contains(msg, "team lead") {
		t.Fatalf("expected lead guard, got ok=%v code=%s msg=%s", ok, code, msg)
	}
	if f.st.resets != 0 || len(f.st.comments) != 0 {
		t.Fatal("store must not be touched")
	}
}

func TestTaskRetry_NoOwnerNeedsAgentID(t *testing.T) {
	f := newHumanTaskFixture(t, store.TeamTaskStatusCancelled)
	f.st.task.OwnerAgentID = nil
	ok, code, msg := f.call(t, f.m.handleTaskRetry, map[string]any{
		"teamId": f.teamID.String(), "taskId": f.taskID.String(), "comment": "go",
	})
	if ok || code != protocol.ErrInvalidRequest || !strings.Contains(msg, "agentId") {
		t.Fatalf("expected agentId requirement, got ok=%v code=%s msg=%s", ok, code, msg)
	}
}
