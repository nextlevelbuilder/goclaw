package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// scheduleArchive removes a task after the archive TTL.
func (sm *SubagentManager) scheduleArchive(taskID string, after time.Duration) {
	time.Sleep(after)
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if t, ok := sm.tasks[taskID]; ok && t.Status != TaskStatusRunning {
		delete(sm.tasks, taskID)
		slog.Debug("subagent archived", "id", taskID)
	}
}

// GetTask returns a task by ID.
func (sm *SubagentManager) GetTask(id string) (*SubagentTask, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	t, ok := sm.tasks[id]
	return t, ok
}

// ListTasks returns all tasks, optionally filtered by parent.
func (sm *SubagentManager) ListTasks(parentID string) []*SubagentTask {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	var result []*SubagentTask
	for _, t := range sm.tasks {
		if parentID == "" || t.ParentID == parentID {
			result = append(result, t)
		}
	}
	return result
}

// CancelTask cancels a running task by ID.
// Special IDs: "all" cancels all running tasks for any parent,
// "last" cancels the most recently created running task.
func (sm *SubagentManager) CancelTask(id string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if id == "all" {
		cancelled := false
		for _, t := range sm.tasks {
			if t.Status == TaskStatusRunning {
				sm.cancelTaskLocked(t)
				cancelled = true
			}
		}
		return cancelled
	}

	if id == "last" {
		var latest *SubagentTask
		for _, t := range sm.tasks {
			if t.Status == TaskStatusRunning {
				if latest == nil || t.CreatedAt > latest.CreatedAt {
					latest = t
				}
			}
		}
		if latest == nil {
			return false
		}
		sm.cancelTaskLocked(latest)
		return true
	}

	t, ok := sm.tasks[id]
	if !ok || t.Status != TaskStatusRunning {
		return false
	}
	sm.cancelTaskLocked(t)
	return true
}

// CancelTasksForParent cancels all running tasks for a specific parent.
func (sm *SubagentManager) CancelTasksForParent(parentID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	count := 0
	for _, t := range sm.tasks {
		if t.ParentID == parentID && t.Status == TaskStatusRunning {
			sm.cancelTaskLocked(t)
			count++
		}
	}
	return count
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
}

// Steer cancels a running subagent and restarts it with a new message.
// Matching TS subagents-tool.ts steer action: cancel → settle → spawn replacement.
func (sm *SubagentManager) Steer(
	ctx context.Context,
	taskID, newMessage string,
	callback AsyncCallback,
) (string, error) {
	sm.mu.Lock()
	t, ok := sm.tasks[taskID]
	if !ok {
		sm.mu.Unlock()
		return "", fmt.Errorf("subagent %q not found", taskID)
	}
	if t.Status != TaskStatusRunning {
		sm.mu.Unlock()
		return "", fmt.Errorf("subagent %q is not running (status=%s)", taskID, t.Status)
	}

	// Capture origin metadata before cancelling
	parentID := t.ParentID
	depth := t.Depth - 1 // Spawn increments depth, so use original
	label := t.Label + " (steered)"
	model := t.Model
	channel := t.OriginChannel
	chatID := t.OriginChatID
	peerKind := t.OriginPeerKind

	// Cancel old task (suppress announce by marking cancelled before unlock)
	sm.cancelTaskLocked(t)
	sm.mu.Unlock()

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
func (sm *SubagentManager) WaitForChildren(ctx context.Context, parentID string, timeoutSec int) ([]*SubagentTask, error) {
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	deadline := time.After(time.Duration(timeoutSec) * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return sm.ListTasks(parentID), ctx.Err()
		case <-deadline:
			return sm.ListTasks(parentID), fmt.Errorf("timeout after %ds waiting for children", timeoutSec)
		case <-ticker.C:
			tasks := sm.ListTasks(parentID)
			allDone := true
			for _, t := range tasks {
				if t.Status == TaskStatusRunning {
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
