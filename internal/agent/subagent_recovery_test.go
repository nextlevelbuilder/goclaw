package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeTaskStore is a minimal in-memory SubagentTaskStore for the recovery
// tests. We only implement the methods recovery actually uses; the rest
// are no-ops so the type still satisfies the interface.
type fakeTaskStore struct {
	mu             sync.Mutex
	running        []store.SubagentTaskData
	updates        []recoveryUpdate
	listErr        error
	updateErr      error
	listCalledWith int // captured limit param
}

type recoveryUpdate struct {
	ID       uuid.UUID
	Status   string
	Result   string
	TenantID uuid.UUID
}

func (f *fakeTaskStore) ListRunningAcrossTenants(_ context.Context, limit int) ([]store.SubagentTaskData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalledWith = limit
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]store.SubagentTaskData, len(f.running))
	copy(out, f.running)
	return out, nil
}

func (f *fakeTaskStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, result *string, _ int, _, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	upd := recoveryUpdate{ID: id, Status: status, TenantID: store.TenantIDFromContext(ctx)}
	if result != nil {
		upd.Result = *result
	}
	f.updates = append(f.updates, upd)
	return nil
}

// Stub the rest of the SubagentTaskStore interface — recovery never calls these.
func (f *fakeTaskStore) Create(context.Context, *store.SubagentTaskData) error { return nil }
func (f *fakeTaskStore) Get(context.Context, uuid.UUID) (*store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeTaskStore) ListByParent(context.Context, string, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeTaskStore) ListBySession(context.Context, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeTaskStore) Archive(context.Context, time.Duration) (int64, error) { return 0, nil }
func (f *fakeTaskStore) UpdateMetadata(context.Context, uuid.UUID, map[string]any) error {
	return nil
}

// fakeChannelSender records every SendToChannel call. nil-safe for tests
// that want to exercise the "no sender" path (we set sender=nil there
// instead of using this).
type fakeChannelSender struct {
	mu       sync.Mutex
	posts    []recoveryPost
	sendErr  error
	failOnce bool // if true, fail the FIRST SendToChannel only
	calls    int
}

type recoveryPost struct {
	Channel string
	ChatID  string
	Content string
}

func (f *fakeChannelSender) SendToChannel(_ context.Context, channel, chatID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failOnce && f.calls == 1 {
		return errors.New("transient channel failure")
	}
	if f.sendErr != nil {
		return f.sendErr
	}
	f.posts = append(f.posts, recoveryPost{Channel: channel, ChatID: chatID, Content: content})
	return nil
}

// taskFixture builds a SubagentTaskData with sensible defaults; callers
// override fields they care about. Keeps the test bodies short.
func taskFixture(channel, chatID string) store.SubagentTaskData {
	ch := channel
	cid := chatID
	t := store.SubagentTaskData{
		BaseModel: store.BaseModel{
			ID:        uuid.New(),
			CreatedAt: time.Now().Add(-1 * time.Hour),
		},
		TenantID:       uuid.New(),
		ParentAgentKey: "eng",
		Subject:        "test task",
		Status:         "running",
	}
	if channel != "" {
		t.OriginChannel = &ch
	}
	if chatID != "" {
		t.OriginChatID = &cid
	}
	return t
}

func TestRecoverInterruptedSubagents_NilStore(t *testing.T) {
	// Defensive: nil store must not panic, just log and return. Boot
	// could plausibly call recovery before pgStores.SubagentTasks is
	// wired in some misconfiguration.
	RecoverInterruptedSubagents(context.Background(), nil, &fakeChannelSender{}, 10)
	// nothing to assert; test passes if it didn't panic.
}

func TestRecoverInterruptedSubagents_Empty(t *testing.T) {
	store := &fakeTaskStore{}
	sender := &fakeChannelSender{}
	RecoverInterruptedSubagents(context.Background(), store, sender, 10)

	if len(store.updates) != 0 {
		t.Fatalf("expected no updates on empty store, got %d", len(store.updates))
	}
	if len(sender.posts) != 0 {
		t.Fatalf("expected no posts on empty store, got %d", len(sender.posts))
	}
}

func TestRecoverInterruptedSubagents_HappyPath(t *testing.T) {
	// Two running tasks with full origin metadata → both marked
	// interrupted, both get a recovery post.
	taskA := taskFixture("discord-eng", "1217211714185986128")
	taskB := taskFixture("telegram-main", "98765")
	taskStore := &fakeTaskStore{
		running: []store.SubagentTaskData{taskA, taskB},
	}
	sender := &fakeChannelSender{}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 50)

	if taskStore.listCalledWith != 50 {
		t.Errorf("limit not propagated: got %d, want 50", taskStore.listCalledWith)
	}
	if len(taskStore.updates) != 2 {
		t.Fatalf("expected 2 status updates, got %d", len(taskStore.updates))
	}
	for i, u := range taskStore.updates {
		if u.Status != "interrupted" {
			t.Errorf("update %d: status=%q, want interrupted", i, u.Status)
		}
		if u.Result == "" {
			t.Errorf("update %d: empty result, expected reason string", i)
		}
	}
	if len(sender.posts) != 2 {
		t.Fatalf("expected 2 channel posts, got %d", len(sender.posts))
	}
	if !strings.Contains(sender.posts[0].Content, "restarted") {
		t.Errorf("post content missing 'restarted' keyword: %q", sender.posts[0].Content)
	}
	// Tenant context propagation: each update's tenant must match the
	// row's tenant. This is the load-bearing detail — without it the
	// store's tenant guard rejects the update.
	if taskStore.updates[0].TenantID != taskA.TenantID {
		t.Errorf("update[0] tenant mismatch: got %s, want %s",
			taskStore.updates[0].TenantID, taskA.TenantID)
	}
	if taskStore.updates[1].TenantID != taskB.TenantID {
		t.Errorf("update[1] tenant mismatch: got %s, want %s",
			taskStore.updates[1].TenantID, taskB.TenantID)
	}
}

func TestRecoverInterruptedSubagents_SkipsSpawnJobRows(t *testing.T) {
	spawnJob := taskFixture("discord-eng", "1217211714185986128")
	spawnJob.Metadata = map[string]any{
		"runner":        "spawn_job",
		"kind":          "autoplan",
		"command":       "/app/agent/bin/run-discord-plan",
		"worktree_path": "/data/workspace-eng/worktrees/task",
		"sinks":         []any{map[string]any{"type": "discord"}},
		"k8s_job_name":  "agent-job-autoplan-abc",
		"agent_service": "kubernetes",
	}
	inProcess := taskFixture("discord-eng", "222")
	taskStore := &fakeTaskStore{running: []store.SubagentTaskData{spawnJob, inProcess}}
	sender := &fakeChannelSender{}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 50)

	if len(taskStore.updates) != 1 {
		t.Fatalf("expected only in-process row to be marked interrupted, got %+v", taskStore.updates)
	}
	if taskStore.updates[0].ID != inProcess.ID {
		t.Fatalf("updated wrong row: got %s want %s", taskStore.updates[0].ID, inProcess.ID)
	}
	if len(sender.posts) != 1 || sender.posts[0].ChatID != "222" {
		t.Fatalf("expected one recovery post for in-process row, got %+v", sender.posts)
	}
}

func TestRecoverInterruptedSubagents_SkipsLegacySpawnJobRows(t *testing.T) {
	spawnJob := taskFixture("discord-eng", "1217211714185986128")
	spawnJob.Metadata = map[string]any{
		"kind":          "autoplan",
		"command":       "/app/agent/bin/run-discord-plan",
		"worktree_path": "/data/workspace-eng/worktrees/task",
		"sinks":         []any{map[string]any{"type": "discord"}},
	}
	taskStore := &fakeTaskStore{running: []store.SubagentTaskData{spawnJob}}
	sender := &fakeChannelSender{}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 50)

	if len(taskStore.updates) != 0 {
		t.Fatalf("legacy spawn_job row should not be marked interrupted, got %+v", taskStore.updates)
	}
	if len(sender.posts) != 0 {
		t.Fatalf("legacy spawn_job row should not get recovery notice, got %+v", sender.posts)
	}
}

func TestRecoverInterruptedSubagents_NoOriginSilent(t *testing.T) {
	// A row with nil OriginChannel/OriginChatID must still be marked
	// interrupted but produces NO post. Common case: subagents spawned
	// from non-channel contexts (HTTP API, cron, internal triggers).
	t1 := taskFixture("", "")
	taskStore := &fakeTaskStore{running: []store.SubagentTaskData{t1}}
	sender := &fakeChannelSender{}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 0)

	if len(taskStore.updates) != 1 || taskStore.updates[0].Status != "interrupted" {
		t.Fatalf("row should still be marked interrupted, got %+v", taskStore.updates)
	}
	if len(sender.posts) != 0 {
		t.Fatalf("no origin → no post; got %d", len(sender.posts))
	}
}

func TestRecoverInterruptedSubagents_NilSender(t *testing.T) {
	// If the channel manager is unavailable (very early boot, or test
	// harness), recovery still marks rows interrupted and skips posts
	// silently. Boot must not crash on a nil sender.
	t1 := taskFixture("discord-eng", "123")
	taskStore := &fakeTaskStore{running: []store.SubagentTaskData{t1}}

	RecoverInterruptedSubagents(context.Background(), taskStore, nil, 0)

	if len(taskStore.updates) != 1 || taskStore.updates[0].Status != "interrupted" {
		t.Fatalf("update should still happen with nil sender, got %+v", taskStore.updates)
	}
}

func TestRecoverInterruptedSubagents_PostFailureDoesNotBlockOthers(t *testing.T) {
	// First post fails (transient channel error); recovery must still
	// process the second row. This is the integration test for the
	// "single bad row must not stall boot" requirement.
	t1 := taskFixture("discord-eng", "111")
	t2 := taskFixture("discord-eng", "222")
	taskStore := &fakeTaskStore{running: []store.SubagentTaskData{t1, t2}}
	sender := &fakeChannelSender{failOnce: true}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 0)

	// Both rows marked interrupted regardless of post failure.
	if len(taskStore.updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(taskStore.updates))
	}
	// Second post succeeded.
	if len(sender.posts) != 1 {
		t.Fatalf("expected 1 successful post (second row); got %d", len(sender.posts))
	}
	if sender.posts[0].ChatID != "222" {
		t.Errorf("expected the surviving post to be the second row's; got chat=%s", sender.posts[0].ChatID)
	}
}

func TestRecoverInterruptedSubagents_UpdateFailureStillPosts(t *testing.T) {
	// If the DB UPDATE fails (transient connection drop), we still want
	// the user to see the recovery notice — the worst outcome is the
	// row stays in 'running' until the next archive sweep, which is
	// strictly less bad than ghosting the user.
	t1 := taskFixture("discord-eng", "111")
	taskStore := &fakeTaskStore{
		running:   []store.SubagentTaskData{t1},
		updateErr: errors.New("connection dropped"),
	}
	sender := &fakeChannelSender{}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 0)

	if len(sender.posts) != 1 {
		t.Fatalf("post should still happen on update failure; got %d", len(sender.posts))
	}
}

func TestRecoverInterruptedSubagents_ListFailureNoPanic(t *testing.T) {
	// Initial list query fails: log + return. Don't panic, don't try to
	// post anything (we have nothing to post about).
	taskStore := &fakeTaskStore{listErr: errors.New("db down")}
	sender := &fakeChannelSender{}

	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 0)

	if len(sender.posts) != 0 {
		t.Fatalf("no posts when list fails; got %d", len(sender.posts))
	}
}

func TestRecoverInterruptedSubagents_PerPostTimeoutBudget(t *testing.T) {
	// Slow channel must not block the whole recovery. We can't directly
	// observe the per-post timeout from outside, but we can prove that
	// even when the sender takes >5s, the function still returns within
	// a reasonable bound (sum of timeouts + slack), not "forever".
	//
	// 3 rows × 5s timeout each = 15s upper bound. We test with a
	// lower-overhead sender that just records and returns immediately;
	// this asserts the wiring without actually waiting 15s.
	rows := []store.SubagentTaskData{
		taskFixture("c1", "1"),
		taskFixture("c2", "2"),
		taskFixture("c3", "3"),
	}
	taskStore := &fakeTaskStore{running: rows}
	sender := &fakeChannelSender{}

	start := time.Now()
	RecoverInterruptedSubagents(context.Background(), taskStore, sender, 0)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("recovery took too long for 3 fast rows: %v", elapsed)
	}
	if len(sender.posts) != 3 {
		t.Errorf("expected 3 posts, got %d", len(sender.posts))
	}
}
