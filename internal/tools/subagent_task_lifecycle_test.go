package tools

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	orchestration "github.com/nextlevelbuilder/goclaw/internal/childrun"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type taskLifecycleUpdate struct {
	rootAgentID uuid.UUID
	id          uuid.UUID
	status      string
	result      string
}

type recordingSubagentTaskStore struct {
	mu            sync.Mutex
	creates       []store.SubagentTaskData
	rows          map[uuid.UUID]store.SubagentTaskData
	updates       chan taskLifecycleUpdate
	metadata      chan map[string]any
	createStarted chan struct{}
	createRelease chan struct{}
	createErr     error
	updateErr     error
	updateHook    func(context.Context, string) error
	updateStarted chan struct{}
	updateRelease chan struct{}
	updateOnce    sync.Once
}

func newRecordingSubagentTaskStore() *recordingSubagentTaskStore {
	return &recordingSubagentTaskStore{
		rows:     make(map[uuid.UUID]store.SubagentTaskData),
		updates:  make(chan taskLifecycleUpdate, 16),
		metadata: make(chan map[string]any, 16),
	}
}

func TestSubagentTaskStatusesFitPersistentStore(t *testing.T) {
	statuses := []string{
		TaskStatusQueued,
		TaskStatusRunning,
		TaskStatusWaiting,
		TaskStatusCompleted,
		TaskStatusFailed,
		TaskStatusCancelled,
	}
	for _, status := range statuses {
		if len(status) > store.SubagentTaskStatusMaxLength {
			t.Fatalf(
				"subagent status %q is %d characters, exceeds persistent limit %d",
				status,
				len(status),
				store.SubagentTaskStatusMaxLength,
			)
		}
	}
	if TaskStatusWaiting != string(orchestration.ChildRunWaitingChild) {
		t.Fatalf(
			"task waiting status %q differs from child-run state %q",
			TaskStatusWaiting,
			orchestration.ChildRunWaitingChild,
		)
	}
}

func (s *recordingSubagentTaskStore) Create(_ context.Context, task *store.SubagentTaskData) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	s.creates = append(s.creates, *task)
	s.rows[task.ID] = *task
	s.mu.Unlock()
	if s.createStarted != nil {
		close(s.createStarted)
		<-s.createRelease
	}
	return nil
}

func (s *recordingSubagentTaskStore) Get(_ context.Context, rootAgentID, id uuid.UUID) (*store.SubagentTaskData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.rows[id]
	if !ok || task.RootAgentID != rootAgentID {
		return nil, nil
	}
	copy := task
	return &copy, nil
}

func (s *recordingSubagentTaskStore) UpdateStatus(
	ctx context.Context,
	rootAgentID uuid.UUID,
	id uuid.UUID,
	status string,
	result *string,
	_ int,
	_, _ int64,
) error {
	value := ""
	if result != nil {
		value = *result
	}
	s.updates <- taskLifecycleUpdate{
		rootAgentID: rootAgentID,
		id:          id,
		status:      status,
		result:      value,
	}
	if s.updateHook != nil {
		if err := s.updateHook(ctx, status); err != nil {
			return err
		}
	}
	if s.updateErr != nil {
		return s.updateErr
	}
	s.mu.Lock()
	if task, ok := s.rows[id]; ok && task.RootAgentID == rootAgentID {
		task.Status = status
		task.Result = result
		s.rows[id] = task
	}
	s.mu.Unlock()
	if s.updateStarted != nil {
		s.updateOnce.Do(func() { close(s.updateStarted) })
		<-s.updateRelease
	}
	return nil
}

func (s *recordingSubagentTaskStore) ListByParent(context.Context, uuid.UUID, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}

func (s *recordingSubagentTaskStore) ListBySession(context.Context, uuid.UUID, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}

func (s *recordingSubagentTaskStore) Archive(context.Context, uuid.UUID, time.Duration, int) (int64, error) {
	return 0, nil
}

func (s *recordingSubagentTaskStore) UpdateMetadata(_ context.Context, rootAgentID, id uuid.UUID, metadata map[string]any) error {
	s.mu.Lock()
	if task, ok := s.rows[id]; ok && task.RootAgentID == rootAgentID {
		if task.Metadata == nil {
			task.Metadata = make(map[string]any)
		}
		for key, value := range metadata {
			task.Metadata[key] = value
		}
		s.rows[id] = task
	}
	s.mu.Unlock()
	s.metadata <- metadata
	return nil
}

type blockingSubagentProvider struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingSubagentProvider) Name() string         { return "blocking" }
func (*blockingSubagentProvider) DefaultModel() string { return "blocking" }
func (p *blockingSubagentProvider) Chat(
	context.Context,
	providers.ChatRequest,
) (*providers.ChatResponse, error) {
	p.started <- struct{}{}
	<-p.release
	return &providers.ChatResponse{Content: "done", FinishReason: "stop"}, nil
}
func (p *blockingSubagentProvider) ChatStream(
	ctx context.Context,
	req providers.ChatRequest,
	_ func(providers.StreamChunk),
) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func TestRootSubagentsHonorMaxChildrenFanout(t *testing.T) {
	provider := &blockingSubagentProvider{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	admission := orchestration.NewChildRunAdmission(2, 4)
	manager := NewSubagentManagerWithAdmission(
		provider,
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 2, MaxSpawnDepth: 3, MaxChildrenPerAgent: 1},
		admission,
	)
	ctx := subagentTestContext("parent")

	if _, err := manager.Spawn(ctx, "parent", 0, "first", "first", "", "test", "chat", "", nil); err != nil {
		t.Fatalf("spawn first: %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("first root child did not start")
	}
	if _, err := manager.Spawn(ctx, "parent", 0, "second", "second", "", "test", "chat", "", nil); err != nil {
		t.Fatalf("spawn second: %v", err)
	}
	select {
	case <-provider.started:
		t.Fatal("second root child bypassed maxChildren fanout")
	case <-time.After(100 * time.Millisecond):
	}

	close(provider.release)
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("queued root child did not start after capacity released")
	}
	if err := admission.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager.Close()
}

func TestRunSyncAdmissionRejectionCreatesNoGhostTask(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 1)
	if err := admission.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManagerWithAdmission(
		provider,
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 2, MaxSpawnDepth: 3, MaxChildrenPerAgent: 2},
		admission,
	)
	taskStore := newRecordingSubagentTaskStore()
	manager.SetTaskStore(taskStore)

	ctx := subagentTestContext("parent")
	if _, _, _, err := manager.RunSync(ctx, "parent", 0, "task", "label", "", "test", "chat"); err == nil {
		t.Fatal("closed admission unexpectedly accepted sync task")
	}
	select {
	case update := <-taskStore.updates:
		t.Fatalf("rejected task persisted an update: %#v", update)
	default:
	}
	taskStore.mu.Lock()
	if len(taskStore.creates) != 0 {
		t.Fatalf("rejected task persisted creates: %#v", taskStore.creates)
	}
	taskStore.mu.Unlock()
	if got := manager.ListTasks(subagentScopeFromContext(ctx), ""); len(got) != 0 {
		t.Fatalf("rejected task remained in memory: %#v", got)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestCancelQueuedTaskPersistsCancelledStatus(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 4)
	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	blocker, err := admission.Enqueue(context.Background(), orchestration.ChildRunConstraints{
		TenantID: store.MasterTenantID,
		TaskID:   "blocker",
	}, func(context.Context, *orchestration.ChildRunLease) {
		close(blockerStarted)
		<-releaseBlocker
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := blocker.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blockerStarted:
	case <-time.After(time.Second):
		t.Fatal("blocker did not start")
	}

	manager := NewSubagentManagerWithAdmission(
		&recordingSubagentProvider{},
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 2, MaxSpawnDepth: 3, MaxChildrenPerAgent: 2},
		admission,
	)
	taskStore := newRecordingSubagentTaskStore()
	manager.SetTaskStore(taskStore)
	ctx := subagentTestContext("parent")
	if _, err := manager.Spawn(ctx, "parent", 0, "queued task", "queued", "", "test", "chat", "", nil); err != nil {
		t.Fatalf("spawn queued task: %v", err)
	}
	scope := subagentScopeFromContext(ctx)
	tasks := manager.ListTasks(scope, "")
	if len(tasks) != 1 || tasks[0].Status != TaskStatusQueued {
		t.Fatalf("queued tasks = %#v", tasks)
	}
	taskStore.mu.Lock()
	if len(taskStore.creates) != 1 || taskStore.creates[0].RootAgentID != scope.RootAgentID {
		t.Fatalf("persisted create root ownership = %#v, want %s", taskStore.creates, scope.RootAgentID)
	}
	taskStore.mu.Unlock()
	if !manager.CancelTask(scope, tasks[0].ID) {
		t.Fatal("queued task was not cancelled")
	}
	select {
	case update := <-taskStore.updates:
		if update.rootAgentID != scope.RootAgentID ||
			update.status != TaskStatusCancelled || update.result != "cancelled by user" {
			t.Fatalf("cancel update = %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation was not persisted")
	}

	close(releaseBlocker)
	<-blocker.Done()
	if err := admission.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager.Close()
}

func TestManagerCloseWaitsForQueuedCancellationPersistence(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 4)
	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	blocker, err := admission.Enqueue(context.Background(), orchestration.ChildRunConstraints{
		TenantID: store.MasterTenantID,
		TaskID:   "blocker",
	}, func(context.Context, *orchestration.ChildRunLease) {
		close(blockerStarted)
		<-releaseBlocker
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := blocker.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blockerStarted:
	case <-time.After(time.Second):
		t.Fatal("blocker did not start")
	}

	manager := NewSubagentManagerWithAdmission(
		&recordingSubagentProvider{},
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 2, MaxSpawnDepth: 3, MaxChildrenPerAgent: 2},
		admission,
	)
	taskStore := newRecordingSubagentTaskStore()
	taskStore.updateStarted = make(chan struct{})
	taskStore.updateRelease = make(chan struct{})
	manager.SetTaskStore(taskStore)
	ctx := subagentTestContext("parent")
	if _, err := manager.Spawn(ctx, "parent", 0, "queued task", "queued", "", "test", "chat", "", nil); err != nil {
		t.Fatalf("spawn queued task: %v", err)
	}
	scope := subagentScopeFromContext(ctx)
	tasks := manager.ListTasks(scope, "")
	if len(tasks) != 1 {
		t.Fatalf("queued tasks = %#v", tasks)
	}
	if !manager.CancelTask(scope, tasks[0].ID) {
		t.Fatal("queued task was not cancelled")
	}
	select {
	case <-taskStore.updateStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal persistence did not start")
	}

	close(releaseBlocker)
	<-blocker.Done()
	if err := admission.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	closeDone := make(chan struct{})
	go func() {
		manager.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("manager closed before terminal persistence finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(taskStore.updateRelease)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("manager did not close after terminal persistence finished")
	}
}

func TestManagerCloseContextTimesOutOnBlockedTerminalPersistence(t *testing.T) {
	manager := NewSubagentManagerWithAdmission(
		&recordingSubagentProvider{},
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 1, MaxSpawnDepth: 1, MaxChildrenPerAgent: 1},
		orchestration.NewChildRunAdmission(1, 1),
	)
	finish, ok := manager.beginLifecycleOperation()
	if !ok {
		t.Fatal("manager rejected lifecycle operation before close")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := manager.CloseContext(ctx)
	if !errors.Is(err, ErrSubagentLifecycleDrainTimeout) {
		t.Fatalf("CloseContext error = %v, want typed drain timeout", err)
	}

	finish()
	if err := manager.CloseContext(context.Background()); err != nil {
		t.Fatalf("CloseContext retry: %v", err)
	}
}

func TestManagerCloseContextDropsPendingAnnounceBeforeLifecycleDrain(t *testing.T) {
	manager := NewSubagentManagerWithAdmission(
		&recordingSubagentProvider{},
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 1, MaxSpawnDepth: 1, MaxChildrenPerAgent: 1},
		orchestration.NewChildRunAdmission(1, 1),
	)
	drained := make(chan struct{}, 1)
	manager.SetAnnounceQueue(NewAnnounceQueue(
		20,
		20,
		func(string, []AnnounceQueueItem, AnnounceMetadata) {
			drained <- struct{}{}
		},
	))
	finish, ok := manager.beginLifecycleOperation()
	if !ok {
		t.Fatal("manager rejected lifecycle operation before close")
	}
	manager.announceQueue.Enqueue(
		"session",
		AnnounceQueueItem{SubagentID: "task"},
		AnnounceMetadata{},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err := manager.CloseContext(ctx)
	cancel()
	if !errors.Is(err, ErrSubagentLifecycleDrainTimeout) {
		t.Fatalf("CloseContext error = %v, want typed drain timeout", err)
	}
	select {
	case <-drained:
		t.Fatal("pending announce drained while lifecycle shutdown was blocked")
	case <-time.After(50 * time.Millisecond):
	}

	finish()
	if err := manager.CloseContext(context.Background()); err != nil {
		t.Fatalf("CloseContext retry: %v", err)
	}
}

func TestSpawnActivationFailureTerminalizesRowAndRemovesMemoryTask(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 4)
	manager := NewSubagentManagerWithAdmission(
		&recordingSubagentProvider{},
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 2, MaxSpawnDepth: 3, MaxChildrenPerAgent: 2},
		admission,
	)
	taskStore := newRecordingSubagentTaskStore()
	taskStore.createStarted = make(chan struct{})
	taskStore.createRelease = make(chan struct{})
	manager.SetTaskStore(taskStore)

	ctx := subagentTestContext("parent")
	spawnDone := make(chan error, 1)
	go func() {
		_, err := manager.Spawn(ctx, "parent", 0, "task", "label", "", "test", "chat", "", nil)
		spawnDone <- err
	}()
	select {
	case <-taskStore.createStarted:
	case <-time.After(time.Second):
		t.Fatal("queued row was not persisted")
	}
	if err := admission.Close(context.Background()); err != nil {
		t.Fatalf("close admission: %v", err)
	}
	close(taskStore.createRelease)
	select {
	case err := <-spawnDone:
		if err == nil {
			t.Fatal("activation unexpectedly succeeded after admission close")
		}
	case <-time.After(time.Second):
		t.Fatal("spawn did not return after activation failure")
	}

	select {
	case update := <-taskStore.updates:
		if update.status != TaskStatusFailed || update.result == "" {
			t.Fatalf("activation rollback update = %#v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("activation failure did not terminalize persisted row")
	}
	if got := manager.ListTasks(subagentScopeFromContext(ctx), ""); len(got) != 0 {
		t.Fatalf("activation failure left in-memory task: %#v", got)
	}
	manager.Close()
}
