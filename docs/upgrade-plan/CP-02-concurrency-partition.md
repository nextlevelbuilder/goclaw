# CP-02: Concurrency-safe Partitioning Per-Invocation

**Pattern**: #4 from Agentic OS analysis
**Priority**: HIGH — speeds up multi-tool turns by 2-5x
**Dependencies**: None (can parallel with CP-01)
**Estimated effort**: 1.5 weeks
**Branch**: `feature/cp-02-concurrency-partition`

---

## Objective

Add per-invocation `IsConcurrencySafe(args)` to the Tool interface.
Replace ToolStage's simple heuristic with greedy batch partitioning.
Same tool type can be safe in one call and exclusive in another based on input.

---

## Step 1: Extend Tool Interface

### 1.1 Create `internal/tools/concurrency.go`

```go
package tools

// ConcurrencyClassifier can be implemented by tools that support
// per-invocation concurrency classification.
//
// If a tool does not implement this interface, it defaults to EXCLUSIVE
// (safe-by-default — conservative choice).
type ConcurrencyClassifier interface {
	// IsConcurrencySafe returns true if this specific invocation
	// (with these specific args) can safely run in parallel with
	// other concurrent-safe tools.
	//
	// Examples:
	//   exec("cat file.txt")       → true  (read-only command)
	//   exec("npm install")        → false (mutating)
	//   read_file("src/main.go")   → true  (always safe)
	//   write_file("src/main.go")  → false (always exclusive)
	IsConcurrencySafe(args map[string]any) bool
}

// IsConcurrencySafeForTool checks if a tool invocation is safe for
// concurrent execution. Handles tools that don't implement the interface.
//
// Safety rules:
// 1. If tool is nil → false (unknown tool)
// 2. If tool doesn't implement ConcurrencyClassifier → check static metadata
// 3. If IsConcurrencySafe panics → false (defensive)
// 4. If input is invalid → false (defensive)
func IsConcurrencySafeForTool(tool Tool, args map[string]any) bool {
	if tool == nil {
		return false
	}

	// Check if tool implements per-invocation classifier
	if classifier, ok := tool.(ConcurrencyClassifier); ok {
		defer func() {
			if r := recover(); r != nil {
				// If IsConcurrencySafe panics, treat as exclusive
			}
		}()
		return classifier.IsConcurrencySafe(args)
	}

	// Fallback: static metadata check
	if meta, ok := tool.(interface{ Metadata() ToolMetadata }); ok {
		return meta.Metadata().IsReadOnly()
	}

	// Default: exclusive (conservative)
	return false
}
```

### 1.2 Implement for read-only tools

For every tool that is always read-only, add the interface.
These tools already have `CapReadOnly` in `inferMetadata()`:

**Pattern — add to each read-only tool file:**

```go
// Example: internal/tools/read_file.go (or wherever ReadFileTool is defined)
func (t *ReadFileTool) IsConcurrencySafe(args map[string]any) bool {
	return true // reading a file is always safe
}
```

**Tools to implement (always true):**
- `read_file`, `list_files`, `read_image`, `read_audio`, `read_video`, `read_document`
- `memory_search`, `memory_get`, `memory_expand`
- `skill_search`, `knowledge_graph_search`
- `sessions_list`, `session_status`, `sessions_history`
- `datetime`, `web_search`, `web_fetch`

### 1.3 Implement for exec tool (conditional)

This is the most important one — same tool, different safety per input.

**Create `internal/tools/exec_readonly_check.go`:**

```go
package tools

import "strings"

// readOnlyCommands is a whitelist of commands known to have no side effects.
// Each entry can have flag restrictions.
var readOnlyCommands = map[string][]string{
	// File reading
	"cat": nil, "head": nil, "tail": nil, "less": nil, "more": nil,
	"wc": nil, "file": nil, "stat": nil, "du": nil, "df": nil,

	// Search
	"grep": {"-r", "-l", "-n", "-c", "-i"}, // only safe flags
	"rg": nil, "ag": nil, "find": nil, "fd": nil,
	"locate": nil, "which": nil, "whereis": nil,

	// Listing
	"ls": nil, "tree": nil, "exa": nil,

	// Git read-only
	"git log": nil, "git status": nil, "git diff": nil,
	"git show": nil, "git blame": nil, "git branch": nil,
	"git tag": nil, "git remote": nil, "git rev-parse": nil,
	"git ls-files": nil, "git shortlog": nil,

	// Go read-only
	"go vet": nil, "go doc": nil, "go list": nil, "go env": nil,
	"go version": nil,

	// Docker read-only
	"docker ps": nil, "docker images": nil, "docker inspect": nil,
	"docker logs": nil, "docker stats": nil,

	// System info
	"uname": nil, "hostname": nil, "uptime": nil, "whoami": nil,
	"env": nil, "printenv": nil, "date": nil, "cal": nil,
	"echo": nil, "printf": nil,

	// Type checkers / linters (read-only analysis)
	"pyright": nil, "mypy": nil, "eslint": {"--no-fix"},
	"golangci-lint": nil, "shellcheck": nil,
}

// dangerousFlags that negate read-only safety even for safe base commands.
var dangerousFlags = []string{
	"-w", "--write", "-i", "--in-place",
	"--delete", "--remove", "--force",
	"-rf", "--exec", "-exec",
}

// IsReadOnlyCommand checks if a shell command is safe for concurrent execution.
func IsReadOnlyCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	// Check for pipes — each segment must be safe
	if strings.Contains(command, "|") {
		segments := strings.Split(command, "|")
		for _, seg := range segments {
			if !IsReadOnlyCommand(strings.TrimSpace(seg)) {
				return false
			}
		}
		return true
	}

	// Check for dangerous flag patterns first
	for _, flag := range dangerousFlags {
		if strings.Contains(command, " "+flag+" ") || strings.HasSuffix(command, " "+flag) {
			return false
		}
	}

	// Extract base command (first word, or first two words for "git log" etc.)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	// Try two-word match first (e.g., "git log")
	if len(parts) >= 2 {
		twoWord := parts[0] + " " + parts[1]
		if _, ok := readOnlyCommands[twoWord]; ok {
			return true
		}
	}

	// Single word match
	_, ok := readOnlyCommands[parts[0]]
	return ok
}
```

**Add to exec tool:**
```go
// internal/tools/exec.go (or wherever the exec/shell tool is)
func (t *ExecTool) IsConcurrencySafe(args map[string]any) bool {
	command, ok := args["command"].(string)
	if !ok {
		return false
	}
	return IsReadOnlyCommand(command)
}
```

---

## Step 2: Partition Algorithm

### 2.1 Create `internal/pipeline/tool_partition.go`

```go
package pipeline

import "github.com/nextlevelbuilder/goclaw/internal/tools"

// ToolBatch groups tool calls by concurrency safety.
type ToolBatch struct {
	IsConcurrent bool
	Calls        []ToolCall // ToolCall type from providers package
}

// PartitionToolCalls groups consecutive concurrent-safe tools into batches.
// Exclusive tools always get their own single-item batch.
//
// Algorithm (greedy):
//   [Read, Read, Grep, Write, Read, Read]
//   → [Read+Read+Grep (concurrent), Write (exclusive), Read+Read (concurrent)]
//
// Max concurrent batch size: 10 (configurable).
func PartitionToolCalls(
	calls []ToolCall,
	registry *tools.Registry,
	maxConcurrent int,
) []ToolBatch {
	if len(calls) == 0 {
		return nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}

	batches := make([]ToolBatch, 0, len(calls))

	for _, tc := range calls {
		tool := registry.Get(tc.Name)
		safe := tools.IsConcurrencySafeForTool(tool, tc.Args)

		lastIdx := len(batches) - 1

		if safe && lastIdx >= 0 && batches[lastIdx].IsConcurrent &&
			len(batches[lastIdx].Calls) < maxConcurrent {
			// Append to existing concurrent batch
			batches[lastIdx].Calls = append(batches[lastIdx].Calls, tc)
		} else {
			// Start new batch
			batches = append(batches, ToolBatch{
				IsConcurrent: safe,
				Calls:        []ToolCall{tc},
			})
		}
	}

	return batches
}
```

### 2.2 Update ToolStage

**File**: `internal/pipeline/tool_stage.go`

Replace the existing parallel/sequential decision with partitioning:

```go
func (s *ToolStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue
	resp := state.Think.LastResponse
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil
	}

	// NEW: Partition into concurrent/exclusive batches
	maxConcurrent := s.deps.Config.MaxToolConcurrency
	if maxConcurrent == 0 {
		maxConcurrent = 10
	}
	batches := PartitionToolCalls(resp.ToolCalls, s.deps.ToolRegistry, maxConcurrent)

	for _, batch := range batches {
		if batch.IsConcurrent && len(batch.Calls) > 1 &&
			s.deps.ExecuteToolRaw != nil && s.deps.ProcessToolResult != nil {
			// Parallel path: I/O parallel, mutation sequential
			if err := s.executeParallel(ctx, state, batch.Calls); err != nil {
				return err
			}
		} else {
			// Sequential path
			for _, tc := range batch.Calls {
				msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
				if err != nil {
					return fmt.Errorf("execute tool %s: %w", tc.Name, err)
				}
				for _, msg := range msgs {
					state.Messages.AppendPending(msg)
				}
				state.Tool.TotalToolCalls++
				if state.Tool.LoopKilled {
					s.result = BreakLoop
					return nil
				}
			}
		}
	}

	return s.checkExitConditions(state)
}
```

---

## Step 3: Sibling Abort Controller

### 3.1 Create `internal/pipeline/sibling_abort.go`

```go
package pipeline

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// SiblingAbortController manages cancellation of parallel tool siblings.
//
// Policy: only exec/shell tool errors cancel siblings.
// Read-only tool errors are independent — one FileRead failure
// should NOT cancel a parallel Grep.
type SiblingAbortController struct {
	cancel  context.CancelFunc
	aborted atomic.Bool
}

func NewSiblingAbortController(parent context.Context) (context.Context, *SiblingAbortController) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, &SiblingAbortController{cancel: cancel}
}

// ToolErrored is called when a parallel tool fails.
// Only exec-family tools trigger sibling cancellation.
func (sac *SiblingAbortController) ToolErrored(toolName string, err error) {
	if !isExecFamilyTool(toolName) {
		slog.Debug("non-exec tool error — siblings continue",
			"tool", toolName, "err", err)
		return
	}

	if sac.aborted.CompareAndSwap(false, true) {
		slog.Warn("exec tool error — cancelling siblings",
			"tool", toolName, "err", err)
		sac.cancel()
	}
}

func isExecFamilyTool(name string) bool {
	switch name {
	case "exec", "bash", "shell", "run_command":
		return true
	}
	return false
}
```

### 3.2 Wire into executeParallel

In `tool_stage.go`, the existing `executeParallel` method should use the abort controller:

```go
func (s *ToolStage) executeParallel(ctx context.Context, state *RunState, calls []ToolCall) error {
	// NEW: Create sibling abort context
	sibCtx, sibAbort := NewSiblingAbortController(ctx)

	var wg sync.WaitGroup
	results := make([]toolRawResult, len(calls))

	for i, tc := range calls {
		wg.Add(1)
		go func(idx int, tc ToolCall) {
			defer wg.Done()
			msg, raw, err := s.deps.ExecuteToolRaw(sibCtx, tc)
			if err != nil {
				sibAbort.ToolErrored(tc.Name, err) // may cancel siblings
			}
			results[idx] = toolRawResult{msg: msg, raw: raw, err: err}
		}(i, tc)
	}
	wg.Wait()

	// Sequential mutation phase (existing)
	for i, tc := range calls {
		r := results[i]
		if r.err != nil && sibAbort.aborted.Load() && !isExecFamilyTool(tc.Name) {
			// This tool was cancelled by sibling abort — inject synthetic error
			r.msg = syntheticCancelledMessage(tc)
		}
		msgs, err := s.deps.ProcessToolResult(ctx, state, tc, r.msg, r.raw)
		// ...
	}
	return nil
}
```

---

## Step 4: Config & Dependencies

### 4.1 Add to PipelineDeps

**File**: `internal/pipeline/deps.go`

```go
type PipelineConfig struct {
	// ... existing fields ...

	MaxToolConcurrency int // Default: 10. Env: GOCLAW_MAX_TOOL_CONCURRENCY
}
```

### 4.2 Add to PipelineDeps

```go
type PipelineDeps struct {
	// ... existing fields ...

	ToolRegistry *tools.Registry // needed for IsConcurrencySafe lookups
}
```

---

## Verification Checklist

- [ ] `IsConcurrencySafe(args)` interface exists in tools package
- [ ] Read-only tools return true
- [ ] Exec tool: `cat file.txt` → true, `rm -rf /` → false
- [ ] Piped commands: `cat file | grep foo` → true (both segments safe)
- [ ] Unknown tools → false (conservative default)
- [ ] Panicking `IsConcurrencySafe` → false (recovered)
- [ ] `PartitionToolCalls`: [Read, Read, Write, Read] → 3 batches
- [ ] Concurrent batch max size = 10
- [ ] Sibling abort: exec error cancels siblings, read error does not
- [ ] Sequential mutation order preserved after parallel I/O

## Test Files

Create:
- `internal/tools/concurrency_test.go`
- `internal/tools/exec_readonly_check_test.go`
- `internal/pipeline/tool_partition_test.go`
- `internal/pipeline/sibling_abort_test.go`
