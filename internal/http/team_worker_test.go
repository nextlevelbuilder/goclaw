package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ── Mock TeamStore ───────────────────────────────────────────

type mockWorkerTeamStore struct {
	mu       sync.Mutex
	tasks    map[uuid.UUID]*store.TeamTaskData
	comments []store.TeamTaskCommentData

	claimErr    error
	completeErr error
	failErr     error
	progressErr error
	renewErr    error
}

func newMockWorkerTeamStore() *mockWorkerTeamStore {
	return &mockWorkerTeamStore{
		tasks: make(map[uuid.UUID]*store.TeamTaskData),
	}
}

func (m *mockWorkerTeamStore) addTask(t *store.TeamTaskData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
}

// TaskStore methods used by TeamWorkerHandler
func (m *mockWorkerTeamStore) ListTasks(_ context.Context, teamID uuid.UUID, _ string, _ string, _ string, _ string, _ string, _ int, _ int) ([]store.TeamTaskData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.TeamTaskData
	for _, t := range m.tasks {
		if t.TeamID == teamID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *mockWorkerTeamStore) GetTask(_ context.Context, taskID uuid.UUID) (*store.TeamTaskData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		return t, nil
	}
	return nil, store.ErrTaskNotFound
}

func (m *mockWorkerTeamStore) ClaimTask(_ context.Context, taskID, agentID, teamID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimErr != nil {
		return m.claimErr
	}
	t, ok := m.tasks[taskID]
	if !ok || t.Status != store.TeamTaskStatusPending {
		return fmt.Errorf("task not available for claiming (already claimed or not pending)")
	}
	t.Status = store.TeamTaskStatusInProgress
	t.OwnerAgentID = &agentID
	return nil
}

func (m *mockWorkerTeamStore) CompleteTask(_ context.Context, taskID, teamID uuid.UUID, result string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.completeErr != nil {
		return m.completeErr
	}
	t, ok := m.tasks[taskID]
	if !ok || t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("task not in progress or not found")
	}
	t.Status = store.TeamTaskStatusCompleted
	t.Result = &result
	return nil
}

func (m *mockWorkerTeamStore) FailTask(_ context.Context, taskID, teamID uuid.UUID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	t, ok := m.tasks[taskID]
	if !ok || t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("task not in progress or not found")
	}
	t.Status = store.TeamTaskStatusFailed
	r := "FAILED: " + errMsg
	t.Result = &r
	return nil
}

func (m *mockWorkerTeamStore) UpdateTaskProgress(_ context.Context, taskID, teamID uuid.UUID, percent int, step string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.progressErr != nil {
		return m.progressErr
	}
	t, ok := m.tasks[taskID]
	if !ok || t.Status != store.TeamTaskStatusInProgress {
		return fmt.Errorf("task not in progress or not found")
	}
	t.ProgressPercent = percent
	t.ProgressStep = step
	return nil
}

func (m *mockWorkerTeamStore) RenewTaskLock(_ context.Context, taskID, teamID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renewErr != nil {
		return m.renewErr
	}
	return nil
}

func (m *mockWorkerTeamStore) AddTaskComment(_ context.Context, c *store.TeamTaskCommentData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, *c)
	return nil
}

// ── Stub methods (not used by worker handler, required by interface) ──

func (m *mockWorkerTeamStore) CreateTeam(context.Context, *store.TeamData) error            { return nil }
func (m *mockWorkerTeamStore) GetTeam(context.Context, uuid.UUID) (*store.TeamData, error)  { return nil, nil }
func (m *mockWorkerTeamStore) GetTeamUnscoped(context.Context, uuid.UUID) (*store.TeamData, error) { return nil, nil }
func (m *mockWorkerTeamStore) UpdateTeam(context.Context, uuid.UUID, map[string]any) error  { return nil }
func (m *mockWorkerTeamStore) DeleteTeam(context.Context, uuid.UUID) error                  { return nil }
func (m *mockWorkerTeamStore) ListTeams(context.Context) ([]store.TeamData, error)          { return nil, nil }
func (m *mockWorkerTeamStore) AddMember(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (m *mockWorkerTeamStore) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error     { return nil }
func (m *mockWorkerTeamStore) ListMembers(context.Context, uuid.UUID) ([]store.TeamMemberData, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListIdleMembers(context.Context, uuid.UUID) ([]store.TeamMemberData, error) { return nil, nil }
func (m *mockWorkerTeamStore) GetTeamForAgent(context.Context, uuid.UUID) (*store.TeamData, error) { return nil, nil }
func (m *mockWorkerTeamStore) KnownUserIDs(context.Context, uuid.UUID, int) ([]string, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListTaskScopes(context.Context, uuid.UUID) ([]store.ScopeEntry, error) { return nil, nil }
func (m *mockWorkerTeamStore) CreateTask(context.Context, *store.TeamTaskData) error        { return nil }
func (m *mockWorkerTeamStore) UpdateTask(context.Context, uuid.UUID, map[string]any) error  { return nil }
func (m *mockWorkerTeamStore) GetTasksByIDs(context.Context, []uuid.UUID) ([]store.TeamTaskData, error) { return nil, nil }
func (m *mockWorkerTeamStore) SearchTasks(context.Context, uuid.UUID, string, int, string) ([]store.TeamTaskData, error) { return nil, nil }
func (m *mockWorkerTeamStore) DeleteTask(context.Context, uuid.UUID, uuid.UUID) error       { return nil }
func (m *mockWorkerTeamStore) DeleteTasks(context.Context, []uuid.UUID, uuid.UUID) ([]uuid.UUID, error) { return nil, nil }
func (m *mockWorkerTeamStore) AssignTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (m *mockWorkerTeamStore) CancelTask(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (m *mockWorkerTeamStore) FailPendingTask(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (m *mockWorkerTeamStore) ReviewTask(context.Context, uuid.UUID, uuid.UUID) error       { return nil }
func (m *mockWorkerTeamStore) ApproveTask(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (m *mockWorkerTeamStore) RejectTask(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (m *mockWorkerTeamStore) ResetTaskStatus(context.Context, uuid.UUID, uuid.UUID) error  { return nil }
func (m *mockWorkerTeamStore) ListActiveTasksByChatID(context.Context, string) ([]store.TeamTaskData, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListTaskComments(context.Context, uuid.UUID) ([]store.TeamTaskCommentData, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListRecentTaskComments(context.Context, uuid.UUID, int) ([]store.TeamTaskCommentData, error) { return nil, nil }
func (m *mockWorkerTeamStore) RecordTaskEvent(context.Context, *store.TeamTaskEventData) error { return nil }
func (m *mockWorkerTeamStore) ListTaskEvents(context.Context, uuid.UUID) ([]store.TeamTaskEventData, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListTeamEvents(context.Context, uuid.UUID, int, int) ([]store.TeamTaskEventData, error) { return nil, nil }
func (m *mockWorkerTeamStore) AttachFileToTask(context.Context, *store.TeamTaskAttachmentData) error { return nil }
func (m *mockWorkerTeamStore) GetAttachment(context.Context, uuid.UUID) (*store.TeamTaskAttachmentData, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListTaskAttachments(context.Context, uuid.UUID) ([]store.TeamTaskAttachmentData, error) { return nil, nil }
func (m *mockWorkerTeamStore) DetachFileFromTask(context.Context, uuid.UUID, string) error  { return nil }
func (m *mockWorkerTeamStore) RecoverAllStaleTasks(context.Context) ([]store.RecoveredTaskInfo, error) { return nil, nil }
func (m *mockWorkerTeamStore) ForceRecoverAllTasks(context.Context) ([]store.RecoveredTaskInfo, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListRecoverableTasks(context.Context, uuid.UUID) ([]store.TeamTaskData, error) { return nil, nil }
func (m *mockWorkerTeamStore) MarkAllStaleTasks(context.Context, time.Time) ([]store.RecoveredTaskInfo, error) { return nil, nil }
func (m *mockWorkerTeamStore) MarkInReviewStaleTasks(context.Context, time.Time) ([]store.RecoveredTaskInfo, error) { return nil, nil }
func (m *mockWorkerTeamStore) FixOrphanedBlockedTasks(context.Context) ([]store.RecoveredTaskInfo, error) { return nil, nil }
func (m *mockWorkerTeamStore) SetTaskFollowup(context.Context, uuid.UUID, uuid.UUID, time.Time, int, string, string, string) error { return nil }
func (m *mockWorkerTeamStore) ClearTaskFollowup(context.Context, uuid.UUID) error           { return nil }
func (m *mockWorkerTeamStore) ListAllFollowupDueTasks(context.Context) ([]store.TeamTaskData, error) { return nil, nil }
func (m *mockWorkerTeamStore) IncrementFollowupCount(context.Context, uuid.UUID, *time.Time) error { return nil }
func (m *mockWorkerTeamStore) ClearFollowupByScope(context.Context, string, string) (int, error) { return 0, nil }
func (m *mockWorkerTeamStore) SetFollowupForActiveTasks(context.Context, uuid.UUID, string, string, time.Time, int, string) (int, error) { return 0, nil }
func (m *mockWorkerTeamStore) HasActiveMemberTasks(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return false, nil }
func (m *mockWorkerTeamStore) GrantTeamAccess(context.Context, uuid.UUID, string, string, string) error { return nil }
func (m *mockWorkerTeamStore) RevokeTeamAccess(context.Context, uuid.UUID, string) error    { return nil }
func (m *mockWorkerTeamStore) ListTeamGrants(context.Context, uuid.UUID) ([]store.TeamUserGrant, error) { return nil, nil }
func (m *mockWorkerTeamStore) ListUserTeams(context.Context, string) ([]store.TeamData, error) { return nil, nil }
func (m *mockWorkerTeamStore) HasTeamAccess(context.Context, uuid.UUID, string) (bool, error) { return false, nil }

// ── Test Helpers ─────────────────────────────────────────────

func setupWorkerTest(t *testing.T) (*mockWorkerTeamStore, *http.ServeMux) {
	t.Helper()
	// No gateway token → admin role (dev mode)
	setupTestToken(t, "")
	setupTestCache(t, nil)

	ms := newMockWorkerTeamStore()
	h := NewTeamWorkerHandler(ms, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return ms, mux
}

func setupWorkerTestWithAuth(t *testing.T) (*mockWorkerTeamStore, *http.ServeMux) {
	t.Helper()
	setupTestToken(t, "gateway-secret")
	setupTestCache(t, nil)

	ms := newMockWorkerTeamStore()
	h := NewTeamWorkerHandler(ms, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return ms, mux
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewBuffer(b)
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse response JSON: %v\nbody: %s", err, w.Body.String())
	}
	return out
}

// ── Test 1: List pending tasks ───────────────────────────────

func TestWorker_ListTasks_Pending(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	pending := &store.TeamTaskData{TeamID: teamID, Subject: "fix bug", Status: store.TeamTaskStatusPending, TaskNumber: 1}
	pending.ID = uuid.New()
	inProgress := &store.TeamTaskData{TeamID: teamID, Subject: "deploy", Status: store.TeamTaskStatusInProgress, TaskNumber: 2}
	inProgress.ID = uuid.New()
	ms.addTask(pending)
	ms.addTask(inProgress)

	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks?status=pending", teamID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	count := int(resp["count"].(float64))
	if count != 1 {
		t.Errorf("count = %d, want 1 (only pending)", count)
	}
}

// ── Test 2: Claim task ───────────────────────────────────────

func TestWorker_ClaimTask_Success(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	agentID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Subject: "implement feature", Status: store.TeamTaskStatusPending,
		TaskNumber: 42, BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"agent_id": agentID.String(), "worker_id": "pc-01"})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/claim", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Verify task is now in_progress
	ms.mu.Lock()
	task := ms.tasks[taskID]
	ms.mu.Unlock()
	if task.Status != store.TeamTaskStatusInProgress {
		t.Errorf("task status = %s, want in_progress", task.Status)
	}
	if task.OwnerAgentID == nil || *task.OwnerAgentID != agentID {
		t.Errorf("task owner = %v, want %s", task.OwnerAgentID, agentID)
	}
}

// ── Test 3: Duplicate claim returns 409 ──────────────────────

func TestWorker_ClaimTask_AlreadyClaimed(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	owner := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusInProgress,
		OwnerAgentID: &owner, BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"agent_id": uuid.New().String(), "worker_id": "pc-02"})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/claim", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

// ── Test 4: Complete task ────────────────────────────────────

func TestWorker_CompleteTask_Success(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusInProgress,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"result": `{"status":"pass"}`, "agent_id": uuid.New().String()})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/complete", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	ms.mu.Lock()
	task := ms.tasks[taskID]
	ms.mu.Unlock()
	if task.Status != store.TeamTaskStatusCompleted {
		t.Errorf("task status = %s, want completed", task.Status)
	}
}

// ── Test 5: Complete already-completed task returns 409 ──────

func TestWorker_CompleteTask_Idempotent409(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusCompleted,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"result": `{"status":"pass"}`})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/complete", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (already completed); body: %s", w.Code, w.Body.String())
	}
}

// ── Test 6: Fail task ────────────────────────────────────────

func TestWorker_FailTask_Success(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusInProgress,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"reason": "timeout after 900s"})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/fail", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	ms.mu.Lock()
	task := ms.tasks[taskID]
	ms.mu.Unlock()
	if task.Status != store.TeamTaskStatusFailed {
		t.Errorf("task status = %s, want failed", task.Status)
	}
}

// ── Test 7: Progress update ──────────────────────────────────

func TestWorker_Progress_Success(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusInProgress,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]any{"percent": 50, "step": "running tests"})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/progress", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	ms.mu.Lock()
	task := ms.tasks[taskID]
	ms.mu.Unlock()
	if task.ProgressPercent != 50 {
		t.Errorf("progress = %d, want 50", task.ProgressPercent)
	}
	if task.ProgressStep != "running tests" {
		t.Errorf("step = %q, want 'running tests'", task.ProgressStep)
	}
}

// ── Test 8: Comment ──────────────────────────────────────────

func TestWorker_Comment_Success(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	agentID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusInProgress,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{
		"content": "worktree created", "agent_id": agentID.String(), "comment_type": "note",
	})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/comment", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(ms.comments))
	}
	if ms.comments[0].Content != "worktree created" {
		t.Errorf("content = %q, want 'worktree created'", ms.comments[0].Content)
	}
}

// ── Test 9: Heartbeat with lock renewal ──────────────────────

func TestWorker_Heartbeat_WithTask(t *testing.T) {
	_, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()

	body := jsonBody(t, map[string]string{
		"worker_id": "pc-01", "current_task_id": taskID.String(),
	})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/heartbeat", teamID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w)
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	if resp["server_time"] == nil {
		t.Error("server_time missing")
	}
}

func TestWorker_Heartbeat_NoTask(t *testing.T) {
	_, mux := setupWorkerTest(t)

	teamID := uuid.New()

	body := jsonBody(t, map[string]string{"worker_id": "pc-01"})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/heartbeat", teamID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Test 10: Get single task ─────────────────────────────────

func TestWorker_GetTask_Success(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Subject: "test task", Status: store.TeamTaskStatusPending,
		BaseModel: store.BaseModel{ID: taskID},
	})

	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s", teamID, taskID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestWorker_GetTask_NotFound(t *testing.T) {
	_, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()

	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s", teamID, taskID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	// ErrTaskNotFound from GetTask triggers error path
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 or 500; body: %s", w.Code, w.Body.String())
	}
}

// ── Test 11: Auth — no token returns 401 ─────────────────────

func TestWorker_Auth_Unauthorized(t *testing.T) {
	_, mux := setupWorkerTestWithAuth(t)

	teamID := uuid.New()
	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks", teamID), nil)
	// No Authorization header
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// ── Test 12: Auth — wrong token returns 401 ──────────────────

func TestWorker_Auth_WrongToken(t *testing.T) {
	_, mux := setupWorkerTestWithAuth(t)

	teamID := uuid.New()
	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks", teamID), nil)
	r.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// ── Test 13: Auth — valid gateway token passes ───────────────

func TestWorker_Auth_ValidGatewayToken(t *testing.T) {
	ms, mux := setupWorkerTestWithAuth(t)

	teamID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Subject: "task", Status: store.TeamTaskStatusPending,
		BaseModel: store.BaseModel{ID: uuid.New()},
	})

	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks", teamID), nil)
	r.Header.Set("Authorization", "Bearer gateway-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// ── Test 14: Invalid team ID returns 400 ─────────────────────

func TestWorker_InvalidTeamID(t *testing.T) {
	_, mux := setupWorkerTest(t)

	r := httptest.NewRequest("GET", "/v1/teams/not-a-uuid/worker/tasks", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── Test 15: Invalid task ID returns 400 ─────────────────────

func TestWorker_InvalidTaskID(t *testing.T) {
	_, mux := setupWorkerTest(t)

	teamID := uuid.New()
	r := httptest.NewRequest("GET", fmt.Sprintf("/v1/teams/%s/worker/tasks/not-a-uuid", teamID), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ── Test 16: Fail on non-in-progress returns 409 ─────────────

func TestWorker_FailTask_NotInProgress(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusPending,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"reason": "test"})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/fail", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

// ── Test 17: Comment with empty content returns 400 ──────────

func TestWorker_Comment_EmptyContent(t *testing.T) {
	ms, mux := setupWorkerTest(t)

	teamID := uuid.New()
	taskID := uuid.New()
	ms.addTask(&store.TeamTaskData{
		TeamID: teamID, Status: store.TeamTaskStatusInProgress,
		BaseModel: store.BaseModel{ID: taskID},
	})

	body := jsonBody(t, map[string]string{"content": ""})
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/teams/%s/worker/tasks/%s/comment", teamID, taskID), body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}
