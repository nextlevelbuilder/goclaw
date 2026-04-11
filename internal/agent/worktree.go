// Package agent — Git worktree isolation for fork agents (CP-05).
// Each fork agent gets a separate Git worktree so parallel agents
// don't conflict on filesystem changes. Merge via standard Git.
package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
)

// Worktree represents an isolated Git worktree for a fork agent.
type Worktree struct {
	Path      string // e.g., /tmp/goclaw-wt-abc12345
	Branch    string // e.g., goclaw/fork/abc12345
	ParentCwd string // original working directory
}

// WorktreeManager creates and cleans up Git worktrees for agent isolation.
type WorktreeManager struct {
	baseDir string
	mu      sync.Mutex
	active  map[string]*Worktree // runID → worktree
}

// NewWorktreeManager creates a manager that places worktrees under baseDir.
func NewWorktreeManager(baseDir string) *WorktreeManager {
	return &WorktreeManager{
		baseDir: baseDir,
		active:  make(map[string]*Worktree),
	}
}

// Create creates a new Git worktree for the given run.
func (wm *WorktreeManager) Create(ctx context.Context, parentCwd, runID string) (*Worktree, error) {
	short := runID
	if len(short) > 12 {
		short = short[:12]
	}

	wt := &Worktree{
		Path:      filepath.Join(wm.baseDir, "wt-"+short),
		Branch:    "goclaw/fork/" + short,
		ParentCwd: parentCwd,
	}

	cmd := exec.CommandContext(ctx, "git", "-C", parentCwd,
		"worktree", "add", wt.Path, "-b", wt.Branch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("create worktree: %s: %w", stderr.String(), err)
	}

	wm.mu.Lock()
	wm.active[runID] = wt
	wm.mu.Unlock()

	return wt, nil
}

// HasChanges checks if the worktree has uncommitted changes.
func (wm *WorktreeManager) HasChanges(ctx context.Context, wt *Worktree) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", wt.Path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("check worktree changes: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// Remove removes the Git worktree and deletes the branch.
func (wm *WorktreeManager) Remove(ctx context.Context, runID string) error {
	wm.mu.Lock()
	wt, ok := wm.active[runID]
	delete(wm.active, runID)
	wm.mu.Unlock()

	if !ok {
		return nil
	}

	// Remove worktree
	cmd := exec.CommandContext(ctx, "git", "-C", wt.ParentCwd,
		"worktree", "remove", wt.Path, "--force")
	if err := cmd.Run(); err != nil {
		// Non-fatal — log and continue
		return fmt.Errorf("remove worktree %s: %w", wt.Path, err)
	}

	// Delete branch
	cmd = exec.CommandContext(ctx, "git", "-C", wt.ParentCwd,
		"branch", "-D", wt.Branch)
	_ = cmd.Run() // best-effort

	return nil
}

// Active returns the number of active worktrees.
func (wm *WorktreeManager) Active() int {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	return len(wm.active)
}

// --- Fork depth context key for anti-recursive guard ---

type forkDepthKeyT struct{}

// ForkDepthFromCtx returns the current fork nesting depth (0 = not in fork).
func ForkDepthFromCtx(ctx context.Context) int {
	if v, ok := ctx.Value(forkDepthKeyT{}).(int); ok {
		return v
	}
	return 0
}

// WithForkDepth returns a context with the fork depth set.
func WithForkDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, forkDepthKeyT{}, depth)
}

// MaxForkDepth is the maximum allowed fork nesting. Prevents exponential explosion.
const MaxForkDepth = 1
