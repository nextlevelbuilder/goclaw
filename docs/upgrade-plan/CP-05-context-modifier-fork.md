# CP-05: Context Modifier Chain + Fork Isolation

**Patterns**: #6 (Context Modifier) + #8 (Fork via Worktree)
**Priority**: MEDIUM
**Dependencies**: None
**Estimated effort**: 2 weeks
**Branch**: `feature/cp-05-context-modifier-fork`

---

## Part A: Context Modifier Chain (Pattern #6)

### Objective
Tools return `ContextModifier` callbacks instead of mutating RunState directly.
Only exclusive tools may return modifiers — concurrent tools are blocked.

### Step 1: Add ContextModifier to tool Result

**File**: `internal/tools/result.go` (or wherever Result is defined)

```go
// ContextModifier is a function that transforms pipeline state after tool execution.
// Only exclusive (non-concurrent) tools may return modifiers.
// Concurrent tools returning modifiers will have them ignored with a warning.
type ContextModifier func(state *RunState) *RunState
```

Add to Result struct:
```go
type Result struct {
	Content         string
	Metadata        map[string]any
	IsError         bool
	ContextModifier ContextModifier // NEW: nil for most tools
}
```

### Step 2: Enforce constraint in ToolStage

**File**: `internal/pipeline/tool_stage.go`

After tool execution, apply modifiers sequentially (exclusive tools only):

```go
func (s *ToolStage) applyContextModifiers(state *RunState, results []toolResult, batch ToolBatch) {
	for _, r := range results {
		if r.result.ContextModifier == nil {
			continue
		}
		if batch.IsConcurrent {
			slog.Warn("concurrent tool returned context modifier — ignoring",
				"tool", r.call.Name)
			continue // BLOCK: prevent race condition
		}
		state = r.result.ContextModifier(state)
	}
}
```

### Step 3: Example usage in file_write tool

```go
func (t *FileWriteTool) Execute(ctx context.Context, args map[string]any) *Result {
	path := args["path"].(string)
	content := args["content"].(string)
	// ... write file ...

	return &Result{
		Content: fmt.Sprintf("File written: %s", path),
		ContextModifier: func(state *RunState) *RunState {
			state.Context.ModifiedFiles = append(state.Context.ModifiedFiles, path)
			return state
		},
	}
}
```

---

## Part B: Fork Isolation via Git Worktree (Pattern #8)

### Objective
When agents need to modify code in parallel, each gets a separate Git worktree.
Permission requests bubble up to parent. Anti-recursive guard prevents fork explosion.

### Step 1: Create WorktreeManager

**File**: `internal/agent/worktree.go`

```go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
)

type Worktree struct {
	Path      string // /tmp/goclaw-wt-<runID>
	Branch    string // goclaw/fork/<runID>
	ParentCwd string // original working directory
}

type WorktreeManager struct {
	baseDir string
	mu      sync.Mutex
	active  map[string]*Worktree // runID → worktree
}

func NewWorktreeManager(baseDir string) *WorktreeManager {
	return &WorktreeManager{
		baseDir: baseDir,
		active:  make(map[string]*Worktree),
	}
}

func (wm *WorktreeManager) Create(ctx context.Context, parentCwd, runID string) (*Worktree, error) {
	wt := &Worktree{
		Path:      filepath.Join(wm.baseDir, "wt-"+runID[:12]),
		Branch:    "goclaw/fork/" + runID[:12],
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

func (wm *WorktreeManager) HasChanges(ctx context.Context, wt *Worktree) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", wt.Path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

func (wm *WorktreeManager) Remove(ctx context.Context, runID string) error {
	wm.mu.Lock()
	wt, ok := wm.active[runID]
	delete(wm.active, runID)
	wm.mu.Unlock()

	if !ok {
		return nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", wt.ParentCwd,
		"worktree", "remove", wt.Path, "--force")
	return cmd.Run()
}
```

### Step 2: Integrate into agent Run

**File**: `internal/agent/loop_run.go`

Add worktree setup when agent definition has `isolation: worktree`:

```go
func (a *agentImpl) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	// Fork isolation check
	if a.config.Isolation == "worktree" && a.worktreeMgr != nil {
		// Anti-recursive guard: max 1 fork depth
		if forkDepth(ctx) >= 1 {
			return nil, fmt.Errorf("cannot fork inside fork — max depth 1")
		}

		wt, err := a.worktreeMgr.Create(ctx, a.workspace, req.RunID)
		if err != nil {
			return nil, fmt.Errorf("worktree: %w", err)
		}
		defer a.worktreeMgr.Remove(ctx, req.RunID)

		// Override workspace for this agent
		ctx = withWorkspaceCwd(ctx, wt.Path)
		ctx = withForkDepth(ctx, forkDepth(ctx)+1)

		// Add notice to system prompt
		req.ExtraSystemPrompt += fmt.Sprintf(
			"\n\nYou are in an isolated git worktree at %s. "+
			"Changes here do not affect the main branch until merged.", wt.Path)
	}

	return a.runPipeline(ctx, req)
}

// Anti-recursive context keys
type forkDepthKey struct{}
func forkDepth(ctx context.Context) int {
	if v, ok := ctx.Value(forkDepthKey{}).(int); ok { return v }
	return 0
}
func withForkDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, forkDepthKey{}, d)
}
```

### Step 3: Permission mode "bubble"

Fork agents should bubble permission requests to parent:

```go
// When fork agent needs permission, delegate to parent's permission handler
if a.config.PermissionMode == "bubble" {
	// Use parent's permission callback instead of local handler
	ctx = withBubblePermission(ctx, req.ParentPermissionCh)
}
```

---

## Verification Checklist

### Context Modifier
- [ ] Exclusive tool returns modifier → applied after execution
- [ ] Concurrent tool returns modifier → ignored with warning
- [ ] Modifiers applied in execution order (deterministic)
- [ ] RunState updated correctly after modifier chain

### Fork Isolation
- [ ] Agent with `isolation: worktree` → Git worktree created
- [ ] Agent sees files in worktree, not main branch
- [ ] Changes in worktree don't affect main branch
- [ ] Worktree cleaned up after agent completes
- [ ] Fork inside fork → rejected (anti-recursive guard)
- [ ] Permission requests bubble to parent terminal
