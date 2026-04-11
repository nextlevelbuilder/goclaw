# CP-01: Context Defense 5 Layers

**Pattern**: #9 from Agentic OS analysis
**Priority**: HIGH — prevents agent memory loss in long sessions
**Dependencies**: None (start here)
**Estimated effort**: 2 weeks
**Branch**: `feature/cp-01-context-defense`

---

## Objective

Upgrade GoClaw's 2-phase PruneStage into a 5-layer escalating context defense system.
Each layer is progressively more expensive. Only escalate when cheaper layers fail.

```
Current:  PruneStage (soft prune 70% + LLM compact 100%)
Target:   Layer 1 (tool truncation) → Layer 2 (microcompact) →
          Layer 3 (auto-compact + boundary) → Layer 4 (reactive) →
          Layer 5 (context collapse)
```

---

## Step 1: Layer 1 — Tool Result Truncation

### Concept
Each tool result has a max size. Oversized results get persisted to disk,
replaced with a pointer + head/tail preview. Cost: ~0.

### 1.1 Create `internal/pipeline/truncate.go`

```go
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// TruncationConfig controls per-tool result size limits.
type TruncationConfig struct {
	// MaxResultChars is the maximum character count before truncation.
	// Default: 30000 (roughly 7500 tokens).
	MaxResultChars int

	// OverflowDir is where full results are persisted.
	// Default: os.TempDir()/goclaw-overflow/
	OverflowDir string

	// PreviewHeadChars and PreviewTailChars control preview size.
	PreviewHeadChars int // Default: 500
	PreviewTailChars int // Default: 500
}

// DefaultTruncationConfig returns sensible defaults.
func DefaultTruncationConfig() TruncationConfig {
	return TruncationConfig{
		MaxResultChars:   30000,
		OverflowDir:      filepath.Join(os.TempDir(), "goclaw-overflow"),
		PreviewHeadChars: 500,
		PreviewTailChars: 500,
	}
}

// TruncateResult checks if content exceeds MaxResultChars.
// If so, persists full content to disk and returns truncated version with pointer.
// If not, returns content unchanged (zero-alloc fast path).
func TruncateResult(content string, cfg TruncationConfig) (truncated string, overflowRef string) {
	if len(content) <= cfg.MaxResultChars {
		return content, "" // fast path — no truncation needed
	}

	// Persist full content to disk
	ref := persistOverflow(content, cfg.OverflowDir)

	head := content[:cfg.PreviewHeadChars]
	tail := content[len(content)-cfg.PreviewTailChars:]

	truncated = fmt.Sprintf(
		"[Truncated: %d chars. Full output saved to %s. Use read_file to access if needed.]\n\n%s\n\n... (%d chars omitted) ...\n\n%s",
		len(content), ref, head, len(content)-cfg.PreviewHeadChars-cfg.PreviewTailChars, tail,
	)
	return truncated, ref
}

func persistOverflow(content string, dir string) string {
	os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("overflow-%s.txt", uuid.New().String()[:8])
	path := filepath.Join(dir, name)
	os.WriteFile(path, []byte(content), 0644)
	return path
}
```

### 1.2 Integrate into tool execution

**File**: `internal/agent/loop_pipeline_tool_callbacks.go`

Find the function that wraps `ExecuteToolCall` or `ExecuteToolRaw`. After each tool
returns a result, apply truncation:

```go
// After tool execution returns result:
if truncCfg := state.Context.TruncationConfig; truncCfg != nil {
    for i, msg := range resultMsgs {
        if msg.Role == "tool" {
            truncated, ref := TruncateResult(msg.Content, *truncCfg)
            resultMsgs[i].Content = truncated
            if ref != "" {
                resultMsgs[i].Metadata["overflow_ref"] = ref
            }
        }
    }
}
```

### 1.3 Wire config

**File**: `internal/pipeline/deps.go` (PipelineDeps struct)

Add field:
```go
TruncationConfig *TruncationConfig // nil = disabled
```

**File**: `internal/agent/loop_pipeline_adapter.go`

Set default:
```go
deps.TruncationConfig = &DefaultTruncationConfig()
```

### Verification
```bash
# Create a tool that returns >30000 chars
# Verify output is truncated with pointer
# Verify full output saved to /tmp/goclaw-overflow/
go test ./internal/pipeline/ -run TestTruncation -v
```

---

## Step 2: Layer 2 — Microcompact

### Concept
Remove stale tool results from old turns. No LLM call needed.
Frees tokens cheaply before resorting to LLM compaction.

### 2.1 Create `internal/pipeline/microcompact.go`

```go
package pipeline

import "fmt"

// MicrocompactConfig controls stale tool result removal.
type MicrocompactConfig struct {
	// StaleAfterTurns: tool results older than N turns get stubbed.
	// Default: 10
	StaleAfterTurns int

	// MinTokensSaved: only compact if we'd free at least this many tokens.
	// Prevents churn for small savings. Default: 500
	MinTokensSaved int
}

func DefaultMicrocompactConfig() MicrocompactConfig {
	return MicrocompactConfig{
		StaleAfterTurns: 10,
		MinTokensSaved:  500,
	}
}

// MicrocompactResult tracks what was removed.
type MicrocompactResult struct {
	MessagesStubbed int
	TokensFreed     int
}

// Microcompact replaces stale tool results with short stubs.
// Operates in-place on the message buffer. Does NOT call LLM.
//
// Logic:
// 1. Count current turn number from messages.
// 2. For each tool result message older than StaleAfterTurns:
//    a. Estimate tokens in original content.
//    b. Replace content with stub: "[Tool result from turn N removed]"
//    c. Track tokens freed.
// 3. Only apply if total tokens freed >= MinTokensSaved.
func Microcompact(buf *MessageBuffer, currentTurn int, cfg MicrocompactConfig) MicrocompactResult {
	var result MicrocompactResult
	candidates := []int{} // indices of stale messages
	totalFreed := 0

	history := buf.History()
	for i, msg := range history {
		if msg.Role != "tool" {
			continue
		}
		age := currentTurn - msg.Turn
		if age <= cfg.StaleAfterTurns {
			continue
		}
		originalTokens := estimateTokens(msg.Content)
		stubTokens := 15 // "[Tool result from turn N removed — re-run tool if needed]"
		freed := originalTokens - stubTokens
		if freed > 0 {
			candidates = append(candidates, i)
			totalFreed += freed
		}
	}

	if totalFreed < cfg.MinTokensSaved {
		return result // not worth it
	}

	for _, idx := range candidates {
		original := history[idx]
		history[idx].Content = fmt.Sprintf(
			"[Tool result from turn %d removed — use the tool again if you need this data]",
			original.Turn,
		)
		result.MessagesStubbed++
	}

	result.TokensFreed = totalFreed
	buf.SetHistory(history)
	return result
}

// estimateTokens gives a rough token count. ~4 chars per token for English.
func estimateTokens(s string) int {
	return len(s) / 4
}
```

### 2.2 Integrate into PruneStage

**File**: `internal/pipeline/prune_stage.go`

Insert BEFORE Phase 1 (soft prune), inside `Execute()`:

```go
func (s *PruneStage) Execute(ctx context.Context, state *RunState) error {
    s.result = Continue
    // ... budget calculation (existing) ...

    // === NEW: Layer 2 — Microcompact ===
    if s.deps.MicrocompactConfig != nil && historyTokens > softThreshold*60/100 {
        mcResult := Microcompact(state.Messages, state.Tool.TotalToolCalls, *s.deps.MicrocompactConfig)
        if mcResult.TokensFreed > 0 {
            slog.Info("microcompact freed tokens",
                "stubbed", mcResult.MessagesStubbed,
                "tokens_freed", mcResult.TokensFreed)
            historyTokens -= mcResult.TokensFreed
            state.Prune.HistoryTokens = historyTokens
        }
    }

    if historyTokens <= softThreshold {
        return nil // microcompact was enough!
    }

    // === Existing Phase 1: soft prune at 70% ===
    // ... existing code ...
```

---

## Step 3: Layer 3 — Auto-compact with Boundary Message

### Concept
Upgrade PruneStage Phase 2 to create a **compact boundary message**.
Messages before the boundary are dropped from API requests, replaced by summary.

### 3.1 Add CompactBoundary to MessageBuffer

**File**: `internal/pipeline/message_buffer.go`

Add:
```go
// CompactBoundary represents a point in history where everything before
// was summarized and dropped. The summary replaces all prior messages.
type CompactBoundary struct {
	Summary   string    // LLM-generated summary of compacted messages
	CreatedAt time.Time
	TurnAt    int       // Turn number when boundary was created
}

// InsertBoundary compacts all history before current point.
// Prior messages are dropped. Summary becomes the first message.
func (buf *MessageBuffer) InsertBoundary(summary string, turn int) {
	buf.mu.Lock()
	defer buf.mu.Unlock()

	buf.boundaries = append(buf.boundaries, CompactBoundary{
		Summary:   summary,
		CreatedAt: time.Now(),
		TurnAt:    turn,
	})

	// Replace history with: [summary_message] + [messages after boundary]
	summaryMsg := Message{
		Role:    "system",
		Content: fmt.Sprintf("<compact-summary>\n%s\n</compact-summary>", summary),
		Turn:    turn,
		IsCompactBoundary: true,
	}

	// Keep only messages from current turn onward
	var kept []Message
	for _, m := range buf.history {
		if m.Turn >= turn {
			kept = append(kept, m)
		}
	}
	buf.history = append([]Message{summaryMsg}, kept...)
}

// ForAPI returns messages suitable for API call.
// Respects compact boundaries — only returns summary + post-boundary messages.
func (buf *MessageBuffer) ForAPI() []Message {
	buf.mu.RLock()
	defer buf.mu.RUnlock()
	// All messages including compact summary are already trimmed
	return append([]Message{}, buf.history...)
}
```

### 3.2 Update PruneStage Phase 2

**File**: `internal/pipeline/prune_stage.go`

Replace the existing compaction call with boundary-aware version:

```go
// Phase 2: LLM compaction with boundary message
if historyTokens > budget {
    if !state.Compact.MemoryFlushedThisCycle && s.memoryFlush != nil {
        s.memoryFlush.Execute(ctx, state)
        state.Compact.MemoryFlushedThisCycle = true
    }

    summary, err := s.deps.CompactMessages(ctx, state.Messages.History(), s.deps.Config.Model)
    if err != nil {
        slog.Error("compaction failed", "err", err)
        s.result = AbortRun
        return err
    }

    // NEW: Insert boundary instead of replacing history directly
    state.Messages.InsertBoundary(summary, state.Tool.TotalToolCalls)

    historyTokens = s.countHistory(state)
    state.Prune.HistoryTokens = historyTokens
    state.Compact.CompactionCount++

    slog.Info("auto-compact completed",
        "compaction_count", state.Compact.CompactionCount,
        "tokens_after", historyTokens)
}
```

---

## Step 4: Layer 4 — Reactive Compact

### Concept
Emergency handler triggered when API returns 413 (prompt_too_long).
Single-shot circuit breaker — only attempts once per run.

### 4.1 Create `internal/pipeline/reactive_compact.go`

```go
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// ReactiveCompactor handles emergency compaction on 413 errors.
// Single-shot: only attempts once per pipeline run (circuit breaker).
type ReactiveCompactor struct {
	attempted atomic.Bool
	compactFn func(ctx context.Context, messages []Message, model string) (string, error)
}

func NewReactiveCompactor(
	compactFn func(ctx context.Context, messages []Message, model string) (string, error),
) *ReactiveCompactor {
	return &ReactiveCompactor{compactFn: compactFn}
}

// HandleError checks if error is prompt_too_long and attempts emergency compact.
// Returns (shouldRetry, error).
//
// Circuit breaker: if already attempted, returns (false, original error).
// This prevents infinite loop: compact → still too large → compact → ...
func (rc *ReactiveCompactor) HandleError(
	ctx context.Context,
	state *RunState,
	err error,
	model string,
) (retry bool, newErr error) {
	if !isPromptTooLongError(err) {
		return false, err
	}

	// Circuit breaker: only try once
	if !rc.attempted.CompareAndSwap(false, true) {
		slog.Warn("reactive compact already attempted — surfacing error")
		return false, fmt.Errorf("prompt too long after reactive compact: %w", err)
	}

	slog.Warn("prompt_too_long — attempting reactive compact")

	summary, compactErr := rc.compactFn(ctx, state.Messages.History(), model)
	if compactErr != nil {
		return false, fmt.Errorf("reactive compact failed: %w (original: %w)", compactErr, err)
	}

	state.Messages.InsertBoundary(summary, state.Tool.TotalToolCalls)

	slog.Info("reactive compact succeeded — retrying API call")
	return true, nil
}

// isPromptTooLongError checks for HTTP 413 or provider-specific prompt length errors.
func isPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// Check common patterns across providers
	return containsAny(s,
		"413",
		"prompt_too_long",
		"prompt is too long",
		"maximum context length",
		"input too long",
		"request too large",
	)
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if len(s) > 0 && len(p) > 0 && contains(s, p) {
			return true
		}
	}
	return false
}
```

### 4.2 Integrate into ThinkStage

**File**: `internal/pipeline/think_stage.go`

In the error handling after `ChatStream` call:

```go
resp, err := s.deps.ChatStream(ctx, req, onChunk)
if err != nil {
    // === NEW: Layer 4 — Reactive compact ===
    if s.reactiveCompactor != nil {
        retry, newErr := s.reactiveCompactor.HandleError(ctx, state, err, model)
        if retry {
            // Set transition reason for logging/metrics
            state.Think.TransitionReason = "reactive_compact_retry"
            // Return nil to let pipeline retry this iteration
            return nil
        }
        err = newErr
    }
    return err
}
```

Add `reactiveCompactor` field to ThinkStage:
```go
type ThinkStage struct {
    deps              *PipelineDeps
    result            StageResult
    reactiveCompactor *ReactiveCompactor // NEW
}
```

Wire in NewDefaultPipeline.

---

## Step 5: Layer 5 — Context Collapse

### Concept
Read-time projection: don't modify messages in memory, only change the VIEW
sent to the API. Cheapest way to reduce context without losing data.

### 5.1 Create `internal/pipeline/context_collapse.go`

```go
package pipeline

// CollapseStrategy defines how old messages are compressed for API view.
type CollapseStrategy int

const (
	CollapseStripToolResults CollapseStrategy = iota // Remove tool result content
	CollapseKeepRoleOnly                             // Keep role + first line only
	CollapseSummarizeBlock                           // Replace with short summary
)

// CollapseRule matches messages by age and applies a strategy.
type CollapseRule struct {
	MinTurnAge int              // Messages older than this many turns
	Strategy   CollapseStrategy
}

// ContextCollapser applies read-time projections to messages before API call.
// Original messages in MessageBuffer are NOT modified.
type ContextCollapser struct {
	Rules []CollapseRule
}

// DefaultCollapser creates rules for progressive collapse:
//   - Messages > 20 turns old: strip tool results
//   - Messages > 40 turns old: keep role + first line only
func DefaultCollapser() *ContextCollapser {
	return &ContextCollapser{
		Rules: []CollapseRule{
			{MinTurnAge: 20, Strategy: CollapseStripToolResults},
			{MinTurnAge: 40, Strategy: CollapseKeepRoleOnly},
		},
	}
}

// Project creates a reduced view of messages for API consumption.
// Returns a new slice — original messages are untouched.
func (cc *ContextCollapser) Project(messages []Message, currentTurn int) []Message {
	if cc == nil || len(cc.Rules) == 0 {
		return messages
	}

	projected := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.IsCompactBoundary {
			projected = append(projected, msg) // always keep boundaries
			continue
		}

		age := currentTurn - msg.Turn
		collapsed := msg // copy

		for _, rule := range cc.Rules {
			if age >= rule.MinTurnAge {
				collapsed = applyCollapse(collapsed, rule.Strategy)
			}
		}

		if collapsed.Content != "" {
			projected = append(projected, collapsed)
		}
	}
	return projected
}

func applyCollapse(msg Message, strategy CollapseStrategy) Message {
	switch strategy {
	case CollapseStripToolResults:
		if msg.Role == "tool" {
			msg.Content = "[tool result collapsed — re-run tool if needed]"
		}
	case CollapseKeepRoleOnly:
		lines := splitFirstLine(msg.Content)
		msg.Content = lines
	}
	return msg
}

func splitFirstLine(s string) string {
	for i, c := range s {
		if c == '\n' && i > 0 {
			return s[:i] + "..."
		}
	}
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
```

### 5.2 Integrate into ThinkStage (before API call)

**File**: `internal/pipeline/think_stage.go`

Before building the API request:
```go
apiMessages := state.Messages.ForAPI()

// === NEW: Layer 5 — Context collapse ===
if s.collapser != nil {
    apiMessages = s.collapser.Project(apiMessages, state.Tool.TotalToolCalls)
}

resp, err := s.deps.ChatStream(ctx, buildRequest(apiMessages, ...), onChunk)
```

---

## Step 6: Add Substates

**File**: `internal/pipeline/substates.go`

Add to existing substates:
```go
type CompactSubstate struct {
	MemoryFlushedThisCycle bool
	CompactionCount        int    // NEW: track number of compactions
	MicrocompactFreed      int    // NEW: tokens freed by microcompact
	ReactiveAttempted      bool   // NEW: circuit breaker state
}
```

---

## Verification Checklist

- [ ] Layer 1: Tool result > 30000 chars → truncated with overflow pointer
- [ ] Layer 1: Tool result < 30000 chars → unchanged (zero-alloc)
- [ ] Layer 2: Tool results > 10 turns old → stubbed
- [ ] Layer 2: If < 500 tokens would be freed → skip (not worth it)
- [ ] Layer 3: Compact boundary message inserted after LLM summary
- [ ] Layer 3: Messages before boundary excluded from API call
- [ ] Layer 4: 413 error → reactive compact → retry → success
- [ ] Layer 4: Second 413 error → circuit breaker → surface error (no infinite loop)
- [ ] Layer 5: Messages > 20 turns old → tool results stripped in API view
- [ ] Layer 5: Original messages in MessageBuffer unchanged
- [ ] Integration: Layers fire in order 1→2→3→4→5 based on severity
- [ ] Metrics: Log tokens freed at each layer for observability

## Test File

Create `internal/pipeline/context_defense_test.go`:
```go
func TestTruncateResult_UnderLimit(t *testing.T) { ... }
func TestTruncateResult_OverLimit(t *testing.T) { ... }
func TestMicrocompact_StaleResults(t *testing.T) { ... }
func TestMicrocompact_MinTokensSaved(t *testing.T) { ... }
func TestInsertBoundary(t *testing.T) { ... }
func TestReactiveCompact_CircuitBreaker(t *testing.T) { ... }
func TestContextCollapse_StripToolResults(t *testing.T) { ... }
func TestContextCollapse_KeepRoleOnly(t *testing.T) { ... }
func TestFullDefenseEscalation(t *testing.T) { ... } // test all 5 layers in sequence
```
