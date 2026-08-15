package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	subagentArchiveSweepInterval = time.Minute
	subagentArchiveBatchSize     = 256
)

func (sm *SubagentManager) startArchiveSweeper() {
	sm.lifecycleMu.Lock()
	defer sm.lifecycleMu.Unlock()
	if sm.lifecycleClosed {
		return
	}
	sm.sweeperOnce.Do(func() {
		sm.mu.Lock()
		sm.sweeperStarted = true
		sm.mu.Unlock()
		go func() {
			ticker := time.NewTicker(subagentArchiveSweepInterval)
			defer ticker.Stop()
			defer close(sm.sweeperDone)
			for {
				select {
				case <-ticker.C:
					sm.sweepArchivedTasks()
				case <-sm.sweeperStop:
					return
				}
			}
		}()
	})
}

func (sm *SubagentManager) sweepArchivedTasks() {
	now := sm.now()
	type persistentSweep struct {
		scope TaskScope
		ttl   time.Duration
	}
	persistent := make(map[string]persistentSweep)

	sm.mu.Lock()
	removed := 0
	for id, task := range sm.tasks {
		if removed >= subagentArchiveBatchSize || !isTerminalTaskStatus(task.Status) ||
			task.CompletedAt == 0 || task.spawnConfig.ArchiveAfterMinutes <= 0 ||
			now.Before(time.UnixMilli(task.CompletedAt).Add(time.Duration(task.spawnConfig.ArchiveAfterMinutes)*time.Minute)) ||
			!taskExecutionDone(task) {
			continue
		}
		delete(sm.tasks, id)
		removed++
		scope := TaskScope{
			TenantID: task.OriginTenantID, RootAgentID: task.RootAgentID, RootAgentKey: task.RootAgentKey,
		}
		persistent[scope.TenantID.String()+":"+scope.RootAgentID.String()] = persistentSweep{
			scope: scope, ttl: time.Duration(task.spawnConfig.ArchiveAfterMinutes) * time.Minute,
		}
	}
	sm.mu.Unlock()

	for _, sweep := range persistent {
		if sm.taskStore == nil || sweep.scope.RootAgentID == uuid.Nil {
			continue
		}
		ctx := store.WithTenantID(context.Background(), sweep.scope.TenantID)
		if _, err := sm.taskStore.Archive(ctx, sweep.scope.RootAgentID, sweep.ttl, subagentArchiveBatchSize); err != nil {
			slog.Warn("subagent archive sweep failed", "root_agent", sweep.scope.RootAgentKey, "error", err)
		}
	}
}

// Close stops the manager-owned archive sweeper and waits for accepted task
// lifecycle work. Gateway owners should use CloseContext so dependency teardown
// remains bounded.
func (sm *SubagentManager) Close() {
	_ = sm.CloseContext(context.Background())
}

func (sm *SubagentManager) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sm.closeOnce.Do(func() {
		sm.lifecycleMu.Lock()
		sm.lifecycleClosed = true
		sm.lifecycleMu.Unlock()

		sm.mu.RLock()
		started := sm.sweeperStarted
		sm.mu.RUnlock()
		close(sm.sweeperStop)
		go func() {
			if started {
				<-sm.sweeperDone
			}
			sm.lifecycleWG.Wait()
			close(sm.closeDone)
		}()
	})
	if sm.announceQueue != nil {
		if err := sm.announceQueue.CloseContext(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrSubagentLifecycleDrainTimeout, err)
		}
	}
	select {
	case <-sm.closeDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrSubagentLifecycleDrainTimeout, ctx.Err())
	}
}

// beginLifecycleOperation registers task ownership before admission or durable
// persistence begins. Gateway shutdown closes this gate, drains admission, and
// then waits for every registered operation before stores and message-bus
// dependencies are closed.
func (sm *SubagentManager) beginLifecycleOperation() (func(), bool) {
	finishes, ok := sm.beginLifecycleOperations(1)
	if !ok {
		return nil, false
	}
	return finishes[0], true
}

func (sm *SubagentManager) beginLifecycleOperations(count int) ([]func(), bool) {
	if count < 1 {
		return nil, false
	}
	sm.lifecycleMu.Lock()
	defer sm.lifecycleMu.Unlock()
	if sm.lifecycleClosed {
		return nil, false
	}
	sm.lifecycleWG.Add(count)
	finishes := make([]func(), count)
	for i := range finishes {
		var once sync.Once
		finishes[i] = func() {
			once.Do(sm.lifecycleWG.Done)
		}
	}
	return finishes, true
}

// GetTask returns an immutable snapshot when the caller owns the root tree.
func (sm *SubagentManager) GetTask(scope TaskScope, id string) (*SubagentTask, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	t, ok := sm.tasks[id]
	if !ok || !taskMatchesScope(t, scope) {
		return nil, false
	}
	return cloneSubagentTask(t), true
}

// ListTasks returns immutable snapshots from one tenant/root-agent tree.
func (sm *SubagentManager) ListTasks(scope TaskScope, parentTaskID string) []*SubagentTask {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var result []*SubagentTask
	for _, t := range sm.tasks {
		if !taskMatchesScope(t, scope) {
			continue
		}
		if parentTaskID == "" || t.ParentTaskID == parentTaskID {
			result = append(result, cloneSubagentTask(t))
		}
	}
	return result
}

// CancelTask cancels a running task by ID.
// Special IDs: "all" cancels all running tasks for any parent,
// "last" cancels the most recently created running task.
func (sm *SubagentManager) CancelTask(scope TaskScope, id string) bool {
	finish, ok := sm.beginLifecycleOperation()
	if !ok {
		return false
	}
	transferred := false
	defer func() {
		if !transferred {
			finish()
		}
	}()

	sm.mu.Lock()
	var cancelled []*SubagentTask

	if id == "all" {
		for _, t := range sm.tasks {
			if taskMatchesScope(t, scope) && isActiveTaskStatus(t.Status) {
				sm.cancelTaskLocked(t)
				cancelled = append(cancelled, t)
			}
		}
		sm.mu.Unlock()
		if len(cancelled) > 0 {
			transferred = true
			go func() {
				defer finish()
				sm.persistCancelledTasks(cancelled)
			}()
		}
		return len(cancelled) > 0
	}

	if id == "last" {
		var latest *SubagentTask
		for _, t := range sm.tasks {
			if taskMatchesScope(t, scope) && isActiveTaskStatus(t.Status) {
				if latest == nil || t.CreatedAt > latest.CreatedAt {
					latest = t
				}
			}
		}
		if latest == nil {
			sm.mu.Unlock()
			return false
		}
		sm.cancelTaskLocked(latest)
		sm.mu.Unlock()
		transferred = true
		go func() {
			defer finish()
			sm.persistCancelledTasks([]*SubagentTask{latest})
		}()
		return true
	}

	t, ok := sm.tasks[id]
	if !ok || !taskMatchesScope(t, scope) || !isActiveTaskStatus(t.Status) {
		sm.mu.Unlock()
		return false
	}
	sm.cancelTaskLocked(t)
	sm.mu.Unlock()
	transferred = true
	go func() {
		defer finish()
		sm.persistCancelledTasks([]*SubagentTask{t})
	}()
	return true
}

// CancelTasksForParent cancels all running tasks for a specific parent.
func (sm *SubagentManager) CancelTasksForParent(scope TaskScope, parentTaskID string) int {
	finish, ok := sm.beginLifecycleOperation()
	if !ok {
		return 0
	}
	transferred := false
	defer func() {
		if !transferred {
			finish()
		}
	}()

	sm.mu.Lock()
	var cancelled []*SubagentTask
	for _, t := range sm.tasks {
		if taskMatchesScope(t, scope) && t.ParentTaskID == parentTaskID && isActiveTaskStatus(t.Status) {
			sm.cancelTaskLocked(t)
			cancelled = append(cancelled, t)
		}
	}
	sm.mu.Unlock()
	if len(cancelled) > 0 {
		transferred = true
		go func() {
			defer finish()
			sm.persistCancelledTasks(cancelled)
		}()
	}
	return len(cancelled)
}

// cancelTaskLocked sets a task to cancelled and fires its context cancel.
// Must be called with sm.mu held.
func (sm *SubagentManager) cancelTaskLocked(t *SubagentTask) {
	t.Status = TaskStatusCancelled
	t.Result = "cancelled by user"
	t.CompletedAt = time.Now().UnixMilli()
	if t.cancelFunc != nil {
		t.cancelFunc()
	}
	if t.admissionTicket != nil {
		t.admissionTicket.Cancel()
	}
}

func (sm *SubagentManager) persistCancelledTasks(tasks []*SubagentTask) {
	for _, task := range tasks {
		ctx := store.WithTenantID(context.Background(), task.OriginTenantID)
		sm.persistStatus(ctx, task, 0)
	}
}

// Steer cancels a running subagent and restarts it with a new message.
// Matching TS subagents-tool.ts steer action: cancel → settle → spawn replacement.
func (sm *SubagentManager) Steer(
	ctx context.Context,
	scope TaskScope,
	taskID, newMessage string,
	callback AsyncCallback,
) (string, error) {
	finishes, ok := sm.beginLifecycleOperations(2)
	if !ok {
		return "", fmt.Errorf("subagent manager is closed")
	}
	finishSteer := finishes[0]
	finishPersistence := finishes[1]
	persistenceTransferred := false
	defer finishSteer()
	defer func() {
		if !persistenceTransferred {
			finishPersistence()
		}
	}()

	sm.mu.Lock()
	t, ok := sm.tasks[taskID]
	if !ok || !taskMatchesScope(t, scope) {
		sm.mu.Unlock()
		return "", fmt.Errorf("subagent %q not found", taskID)
	}
	if !isActiveTaskStatus(t.Status) {
		sm.mu.Unlock()
		return "", fmt.Errorf("subagent %q is not running (status=%s)", taskID, t.Status)
	}

	// Capture origin metadata before cancelling
	parentID := t.RootAgentKey
	depth := t.Depth - 1 // Spawn increments depth, so use original
	label := t.Label + " (steered)"
	model := t.Model
	channel := t.OriginChannel
	chatID := t.OriginChatID
	peerKind := t.OriginPeerKind

	// Cancel old task (suppress announce by marking cancelled before unlock)
	sm.cancelTaskLocked(t)
	sm.mu.Unlock()
	persistenceTransferred = true
	go func() {
		defer finishPersistence()
		sm.persistCancelledTasks([]*SubagentTask{t})
	}()

	// Brief settle period (matching TS 500ms settle)
	time.Sleep(500 * time.Millisecond)

	// Truncate message to 4000 chars (matching TS MAX_STEER_MESSAGE_LENGTH)
	if len(newMessage) > 4000 {
		newMessage = newMessage[:4000]
	}

	// Spawn replacement
	msg, err := sm.Spawn(ctx, parentID, depth, newMessage, label, model,
		channel, chatID, peerKind, callback)
	if err != nil {
		return "", fmt.Errorf("steer respawn failed: %w", err)
	}

	return fmt.Sprintf("Steered subagent %q → new task spawned. %s", taskID, msg), nil
}

// WaitForChildren blocks until all running tasks for parentID complete or timeout.
func (sm *SubagentManager) WaitForChildren(ctx context.Context, scope TaskScope, parentTaskID string, timeoutSec int) ([]*SubagentTask, error) {
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	deadline := time.After(time.Duration(timeoutSec) * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return sm.ListTasks(scope, parentTaskID), ctx.Err()
		case <-deadline:
			return sm.ListTasks(scope, parentTaskID), fmt.Errorf("timeout after %ds waiting for children", timeoutSec)
		case <-ticker.C:
			tasks := sm.ListTasks(scope, parentTaskID)
			allDone := true
			for _, t := range tasks {
				if isActiveTaskStatus(t.Status) {
					allDone = false
					break
				}
			}
			if allDone {
				return tasks, nil
			}
		}
	}
}

func taskMatchesScope(task *SubagentTask, scope TaskScope) bool {
	return task != nil &&
		scope.TenantID != uuid.Nil &&
		scope.RootAgentID != uuid.Nil &&
		scope.RootAgentKey != "" &&
		task.OriginTenantID == scope.TenantID &&
		task.RootAgentID == scope.RootAgentID &&
		task.RootAgentKey == scope.RootAgentKey
}

func isActiveTaskStatus(status string) bool {
	return status == TaskStatusQueued || status == TaskStatusRunning || status == TaskStatusWaiting
}

func isTerminalTaskStatus(status string) bool {
	return status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled
}

func taskExecutionDone(task *SubagentTask) bool {
	if task.admissionTicket == nil {
		return true
	}
	select {
	case <-task.admissionTicket.Done():
		return true
	default:
		return false
	}
}

func cloneSubagentTask(task *SubagentTask) *SubagentTask {
	if task == nil {
		return nil
	}
	snapshot := *task
	snapshot.Media = append([]bus.MediaFile(nil), task.Media...)
	snapshot.cancelFunc = nil
	snapshot.admissionTicket = nil
	return &snapshot
}

func generateSubagentID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "sub-" + hex.EncodeToString(b)
}

func truncate(s string, maxLen int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxLen {
		return s
	}
	// Don't cut in the middle of a multi-byte rune
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen] + "..."
}
