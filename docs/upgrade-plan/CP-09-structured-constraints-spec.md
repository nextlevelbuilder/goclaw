# CP-09: Structured Constraints — Implementation-Grade Architecture Spec

**Status**: Implementation-ready
**Supersedes**: CP-09-capability-gating-state-policy.md (design note), 2026-04-12-branch-viability-control-design-review.md (critique)
**Author**: Claude Opus 4.6
**Reviewed against**: Production code as of commit `14726bb0` (April 12, 2026)

---

## 1. Problem Statement

Three classes of production failures share one root cause:

| Class | Example | Root cause |
|-------|---------|------------|
| Dead prerequisite | `git` missing → agent retries `git clone` | Error is free-text, not structured. Runtime doesn't block next call. |
| Exhausted capacity | Spawn 5/5 → agent retries spawn | Capacity boundary exists at tool level but doesn't propagate upward. |
| Low-signal loop | Fetch GitHub page → get HTML chrome → re-fetch | No quality feedback mechanism from tool to runtime. |

**One fix**: Tools emit typed constraints. Runtime stores them sticky. Runtime blocks pre-call + injects into system prompt. LLM sees constraints and self-routes.

---

## 2. Design Principles

1. **Tool emits, runtime enforces.** The tool knows its failure context best. Runtime stores and acts on it.
2. **Constraint, not symbolic plan.** No ActionFamily taxonomy. LLM decides *what* to do; runtime decides *what's forbidden*.
3. **Additive, not breaking.** Tools without constraints behave exactly as today. Pipeline without ConstraintStore = status quo.
4. **Deterministic transitions.** Every state change has a defined precedence. No ambiguous multi-signal merges.
5. **Ship-then-iterate.** V1 uses deterministic rules only. LLM-based classification is explicitly Phase 2.

---

## 3. Transition Precedence Table

### 3.1 Turn Phase Transitions

Turn phase is the top-level outcome state (already exists in `turn_state.go`).
New constraint signals feed INTO the existing `TurnState.ArmCloseout()` path.

**Precedence rule: higher row wins. Once a turn reaches a terminal phase, it cannot regress.**

| Priority | From | To | Trigger | Action |
|----------|------|-----|---------|--------|
| **P0** | any | `blocked` | Constraint with `Severity=hard` added AND `affects_all_tools=true` | `ArmCloseout(blocked)`. Inject system message. No more tools. |
| **P1** | `running` | `needs_human` | Constraint with `Resolution=human_required` added | `ArmCloseout(needs_human)`. Inject "ask user" message. |
| **P2** | `running` | `partial` | ≥2 consecutive tool calls blocked by constraints (different tools) | `ArmCloseout(partial)`. LLM must answer from evidence so far. |
| **P3** | `running` | `running` | Constraint added but alternative tools still available | Log constraint. Inject into system prompt. LLM self-routes. |
| **P4** | `running` | `completed` | LLM returns no tool calls (existing `BreakLoop`) | Normal completion. |

**Terminal phases (cannot regress):** `blocked`, `needs_human`, `completed`.
**Non-terminal:** `running` (can transition to anything), `partial` (can upgrade to `blocked`).

```
running ──→ running     (P3: constraint added, alternatives exist)
running ──→ partial     (P2: ≥2 consecutive blocked calls)
running ──→ blocked     (P0: hard constraint, no alternatives)
running ──→ needs_human (P1: human resolution required)
running ──→ completed   (P4: normal LLM completion)
partial ──→ blocked     (P0: escalation from partial)
partial ──→ completed   (P4: LLM answers from partial evidence)
```

### 3.2 Constraint-to-Phase Mapping

| ConstraintKind | Default Severity | Default Resolution | Typical Phase Effect |
|---------------|-----------------|-------------------|---------------------|
| `binary_missing` | `hard` | `human_required` | → `needs_human` (user must install) |
| `capacity_exhausted` | `hard` | `self_reroute` | → `running` (alternative tools exist) |
| `policy_blocked` | `hard` | `human_required` | → `blocked` or `needs_human` |
| `low_signal` | `soft` | `self_reroute` | → `running` (try different approach) |
| `auth_required` | `hard` | `human_required` | → `needs_human` |
| `resource_unavailable` | `soft` | `self_reroute` | → `running` (try alternative) |
| `repeated_failure` | `soft` | `self_reroute` | → `partial` after 2+ blocked |

### 3.3 Reroute vs Block Decision

When a constraint is added, the runtime decides **reroute** or **block** based on a simple test:

```
IF constraint.Resolution == human_required:
    → needs_human (BLOCK — only human can fix)

ELSE IF hasAlternativeTools(state, constraint):
    → running (REROUTE — LLM picks alternative)

ELSE IF consecutiveBlockedCalls >= 2:
    → partial (SOFT BLOCK — answer from evidence)

ELSE:
    → running (first blocked call — give LLM one more chance)
```

`hasAlternativeTools` is deterministic: "are there tool calls in the last response that are NOT blocked by any constraint?" If yes → alternatives exist.

---

## 4. Novelty Contract (V1 — Deterministic Rules)

### 4.1 Problem

LLM fetches the same low-value content repeatedly. The runtime needs to detect "this tool call adds no new information" without calling an LLM to judge.

### 4.2 V1 Novelty Rules (Deterministic Only)

| Rule | Detection method | Threshold | Result |
|------|-----------------|-----------|--------|
| **Exact repeat** | `hash(tool_name + args) == previous hash` | 1 repeat | Block call, inject "[System] exact duplicate — skipped" |
| **Same-tool-same-target** | Same tool + same primary arg (URL, file path) | 2 repeats | Emit `Constraint{Kind: repeated_failure, Subject: target}` |
| **Content similarity** | `len(result) > 0 && result == previous_result_for_same_tool` | 1 repeat | Emit `Constraint{Kind: low_signal, Subject: target}` |
| **Error repeat** | Same tool + same error signature (first 100 chars) | 2 repeats | Emit `Constraint{Kind: repeated_failure}` + escalate severity |
| **Diminishing returns** | Same tool type, result size shrinking across calls | 3 calls with shrinking results | Emit `Constraint{Kind: low_signal}` |

### 4.3 Novelty State (per tool-target pair)

```go
type NoveltyTracker struct {
    mu      sync.RWMutex
    entries map[string]*NoveltyEntry // key = tool_name + ":" + primary_arg
}

type NoveltyEntry struct {
    CallCount       int
    LastArgsHash    uint64
    LastResultHash  uint64
    LastResultLen   int
    LastErrorSig    string  // first 100 chars of error
    ConsecutiveSame int     // same result hash in a row
    ShrinkingCount  int     // consecutive result-size decreases
}
```

### 4.4 Novelty Check Integration Point

Runs in ToolStage **after** tool execution, **before** appending result to messages:

```go
// In processRawResult or ExecuteToolCall callback:
entry := state.Tool.Novelty.Record(tc.Name, tc.Args, result)

if entry.ConsecutiveSame >= 2 {
    state.Tool.Constraints.Add(Constraint{
        Kind:    ConstraintLowSignal,
        Subject: extractTarget(tc),
        Message: "repeated identical results — try a different approach",
        Sticky:  false, // clears next turn (target might change)
    })
}

if entry.CallCount >= 3 && entry.ShrinkingCount >= 2 {
    state.Tool.Constraints.Add(Constraint{
        Kind:    ConstraintLowSignal,
        Subject: extractTarget(tc),
        Message: "diminishing returns — each call gets less useful",
        Sticky:  false,
    })
}
```

### 4.5 What V1 Does NOT Do

- No LLM-based content quality scoring
- No semantic similarity (embedding distance)
- No cross-tool novelty comparison
- No "signal score" per artifact

These are all Phase 2 candidates, gated behind production data showing V1 rules miss real cases.

---

## 5. Separation: Intent (what LLM wants) vs Control (what runtime allows)

### 5.1 The Wrong Way (ActionFamily)

```
LLM intent → ActionFamily classifier → BranchPolicyEngine → tool filter
```

Problems:
- Runtime must infer LLM intent (fragile)
- Pre-defined family taxonomy (brittle, doesn't scale)
- Runtime decides WHAT to do (conflicts with LLM's role)

### 5.2 The Right Way (Constraint Fence)

```
LLM intent → tool call → constraint check → ALLOW or BLOCK
                                              ↓ (if blocked)
                                     inject reason into conversation
                                              ↓
                                     LLM sees reason → self-reroutes
```

**Separation**:

| Concern | Owner | Mechanism |
|---------|-------|-----------|
| "What should I do next?" | **LLM** | Reasoning over conversation + constraints |
| "Is this tool call permitted right now?" | **Runtime** | ConstraintStore pre-call check |
| "Why was it blocked?" | **Runtime** | Structured constraint message in conversation |
| "What alternative should I try?" | **LLM** | Reasoning over the block reason |

**The runtime never decides what the LLM should do instead.** It only says what's currently impossible and why. The LLM handles rerouting.

### 5.3 Why This Works Better

In Session A (git missing):
- **ActionFamily approach**: Runtime classifies intent as `repo_bootstrap_local`, blocks it, must know to suggest `repo_inspect_remote` or `explain_setup`
- **Constraint approach**: Runtime says "git is missing". LLM decides what to do — might inspect remote, might explain setup, might ask user. Runtime doesn't need to enumerate alternatives.

In Session C (spawn quota):
- **ActionFamily approach**: Runtime classifies intent as `spawn_more_workers`, blocks it, must activate `coordinator_local_synthesis`
- **Constraint approach**: Runtime says "spawn children 5/5". LLM decides — might synthesize locally, might wait for existing children, might ask user. Runtime doesn't prescribe.

**The LLM is better at choosing alternatives than any static taxonomy.** The runtime is better at knowing what's impossible.

---

## 6. Data Model

### 6.1 Constraint Types

```go
// internal/pipeline/constraint.go

type ConstraintKind string

const (
    ConstraintBinaryMissing     ConstraintKind = "binary_missing"
    ConstraintCapacityExhausted ConstraintKind = "capacity_exhausted"
    ConstraintPolicyBlocked     ConstraintKind = "policy_blocked"
    ConstraintLowSignal         ConstraintKind = "low_signal"
    ConstraintAuthRequired      ConstraintKind = "auth_required"
    ConstraintResourceUnavail   ConstraintKind = "resource_unavailable"
    ConstraintRepeatedFailure   ConstraintKind = "repeated_failure"
)

type ConstraintSeverity string

const (
    SeverityHard ConstraintSeverity = "hard"  // tool MUST NOT execute
    SeveritySoft ConstraintSeverity = "soft"  // tool CAN execute but result likely useless
)

type ConstraintResolution string

const (
    ResolutionSelfReroute  ConstraintResolution = "self_reroute"   // LLM can find alternative
    ResolutionHumanRequired ConstraintResolution = "human_required" // only user can fix
)

type Constraint struct {
    Kind       ConstraintKind
    Subject    string              // "git", "spawn.children", "https://..."
    Message    string              // human-readable explanation
    Severity   ConstraintSeverity  // hard or soft
    Resolution ConstraintResolution // self_reroute or human_required
    Sticky     bool                // persist across iterations
    AddedAt    int                 // iteration number when added
}
```

### 6.2 ConstraintStore

```go
// internal/pipeline/constraint_store.go

type ConstraintStore struct {
    mu      sync.RWMutex
    entries map[string]Constraint // key = kind + ":" + subject

    // Transition tracking
    consecutiveBlocked int // consecutive tool calls blocked by constraints
}

// Add registers a constraint. If same key exists, higher severity wins.
func (cs *ConstraintStore) Add(c Constraint) {
    cs.mu.Lock()
    defer cs.mu.Unlock()
    key := string(c.Kind) + ":" + c.Subject
    if existing, ok := cs.entries[key]; ok {
        if severityRank(c.Severity) <= severityRank(existing.Severity) {
            return // existing is equal or higher severity
        }
    }
    cs.entries[key] = c
}

// Check returns (blocked, constraint) for a specific tool call.
func (cs *ConstraintStore) Check(toolName string, args map[string]any) (bool, *Constraint) {
    cs.mu.RLock()
    defer cs.mu.RUnlock()

    for _, c := range cs.entries {
        if c.Severity != SeverityHard {
            continue
        }
        if matchesToolCall(c, toolName, args) {
            return true, &c
        }
    }
    return false, nil
}

// RecordBlocked increments consecutive blocked counter.
// Returns the new count (used for P2 transition check).
func (cs *ConstraintStore) RecordBlocked() int {
    cs.mu.Lock()
    defer cs.mu.Unlock()
    cs.consecutiveBlocked++
    return cs.consecutiveBlocked
}

// RecordAllowed resets consecutive blocked counter.
func (cs *ConstraintStore) RecordAllowed() {
    cs.mu.Lock()
    defer cs.mu.Unlock()
    cs.consecutiveBlocked = 0
}

// ClearNonSticky removes all non-sticky constraints.
// Called at the start of each iteration.
func (cs *ConstraintStore) ClearNonSticky() { ... }

// ForSystemPrompt formats active constraints for LLM visibility.
func (cs *ConstraintStore) ForSystemPrompt() string {
    cs.mu.RLock()
    defer cs.mu.RUnlock()
    if len(cs.entries) == 0 {
        return ""
    }
    var sb strings.Builder
    sb.WriteString("\n[Active environment constraints — do NOT retry these]\n")
    for _, c := range cs.entries {
        icon := "⚠"
        if c.Severity == SeverityHard { icon = "🚫" }
        sb.WriteString(fmt.Sprintf("- %s %s: %s — %s\n", icon, c.Kind, c.Subject, c.Message))
        if c.Resolution == ResolutionHumanRequired {
            sb.WriteString("  → Requires human action to resolve.\n")
        }
    }
    return sb.String()
}

// ForTrace returns structured data for trace metadata.
func (cs *ConstraintStore) ForTrace() []map[string]any { ... }
```

### 6.3 matchesToolCall — Constraint→Tool Matching

```go
// matchesToolCall determines if a constraint blocks a specific tool call.
// This is the ONLY place where constraint-to-tool mapping logic lives.
func matchesToolCall(c Constraint, toolName string, args map[string]any) bool {
    switch c.Kind {
    case ConstraintBinaryMissing:
        if toolName != "exec" && toolName != "bash" {
            return false
        }
        cmd, _ := args["command"].(string)
        return commandUsesBinary(cmd, c.Subject)

    case ConstraintCapacityExhausted:
        switch c.Subject {
        case "spawn.children", "spawn.concurrent", "spawn.depth":
            return toolName == "spawn"
        }

    case ConstraintPolicyBlocked:
        // Subject is the blocked command pattern
        if toolName != "exec" && toolName != "bash" {
            return false
        }
        cmd, _ := args["command"].(string)
        return strings.HasPrefix(cmd, c.Subject)

    case ConstraintLowSignal:
        // Subject is the URL or file path
        if toolName == "web_fetch" || toolName == "read_file" {
            target, _ := args["url"].(string)
            if target == "" {
                target, _ = args["path"].(string)
            }
            return target == c.Subject
        }

    case ConstraintRepeatedFailure:
        // Subject is tool_name:primary_arg
        target := extractTarget(toolName, args)
        return c.Subject == target

    case ConstraintAuthRequired:
        // Block all tools that need the auth subject
        return toolName == c.Subject || strings.HasPrefix(c.Subject, toolName+".")
    }

    return false
}

func commandUsesBinary(cmd, binary string) bool {
    base := extractBaseCommand(cmd)
    return base == binary || strings.HasPrefix(cmd, binary+" ")
}
```

### 6.4 NoveltyTracker

```go
// internal/pipeline/novelty_tracker.go

type NoveltyTracker struct {
    mu      sync.RWMutex
    entries map[string]*NoveltyEntry
}

type NoveltyEntry struct {
    CallCount       int
    LastArgsHash    uint64   // FNV hash of marshaled args
    LastResultHash  uint64   // FNV hash of result content
    LastResultLen   int
    LastErrorSig    string
    ConsecutiveSame int      // same result hash in a row
    ShrinkingCount  int      // consecutive shrinks in result length
}

// Record tracks a tool call and returns the updated entry.
// Caller uses entry fields to decide whether to emit a constraint.
func (nt *NoveltyTracker) Record(
    toolName string,
    args map[string]any,
    resultContent string,
    isError bool,
) *NoveltyEntry { ... }

// CheckExactRepeat returns true if this exact (tool+args) was called before.
func (nt *NoveltyTracker) CheckExactRepeat(toolName string, args map[string]any) bool { ... }
```

---

## 7. Integration Points (Exact Locations)

### 7.1 ConstraintStore in RunState

**File**: `internal/pipeline/substates.go`

```go
type ToolState struct {
    // ... existing fields ...
    Constraints *ConstraintStore   // NEW: sticky constraint state
    Novelty     *NoveltyTracker    // NEW: novelty tracking
}
```

**File**: `internal/pipeline/run_state.go` — in `NewRunState()`:

```go
Tool: ToolState{
    Constraints: NewConstraintStore(),
    Novelty:     NewNoveltyTracker(),
},
```

### 7.2 Pre-call Check in ToolStage

**File**: `internal/pipeline/tool_stage.go`

In `executeSequential()`, before `ExecuteToolCall`:

```go
for _, tc := range toolCalls {
    // NEW: Pre-call constraint check
    if blocked, c := state.Tool.Constraints.Check(tc.Name, tc.Args); blocked {
        state.Tool.Constraints.RecordBlocked()
        // Inject block message instead of executing
        state.Messages.AppendPending(providers.Message{
            Role:    "system",
            Content: fmt.Sprintf("[Tool %q blocked] %s: %s. Do not retry — choose an alternative approach.", tc.Name, c.Kind, c.Message),
        })
        // Transition check (Section 3)
        s.checkConstraintTransition(state, c)
        state.Tool.TotalToolCalls++
        continue
    }
    state.Tool.Constraints.RecordAllowed()

    msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
    // ... existing code ...
}
```

### 7.3 Post-call Novelty Check

**File**: `internal/agent/loop_pipeline_tool_callbacks.go`

In `makeExecuteToolCall` or `makeProcessToolResult`, after getting tool result:

```go
// NEW: Novelty tracking + constraint emission
entry := state.Tool.Novelty.Record(tc.Name, tc.Args, result.ForLLM, result.IsError)

if entry.ConsecutiveSame >= 2 {
    state.Tool.Constraints.Add(Constraint{
        Kind:       ConstraintLowSignal,
        Subject:    extractTarget(tc.Name, tc.Args),
        Severity:   SeveritySoft,
        Resolution: ResolutionSelfReroute,
        Message:    "repeated identical results",
    })
}
```

### 7.4 Tool-Level Constraint Emission

**File**: `internal/tools/subagent_spawn.go` — at line 64 (existing capacity check):

```go
if childCount >= cfg.MaxChildrenPerAgent {
    // Existing error return + NEW: emit constraint
    result := &Result{
        ForLLM:  fmt.Sprintf("max children per agent reached (%d/%d)", childCount, max),
        IsError: true,
        Constraints: []Constraint{{
            Kind:       ConstraintCapacityExhausted,
            Subject:    "spawn.children",
            Severity:   SeverityHard,
            Resolution: ResolutionSelfReroute,
            Message:    fmt.Sprintf("child limit %d/%d — spawn is blocked", childCount, max),
            Sticky:     true,
        }},
    }
    return result
}
```

**File**: `internal/agent/exec_probe_recovery.go` — replace streak heuristic:

```go
// Instead of counting probes, emit constraint on first decisive miss
if isReadOnlyExecProbeMiss(result) {
    binaryName := extractProbedBinary(command)
    if binaryName != "" {
        state.Tool.Constraints.Add(Constraint{
            Kind:       ConstraintBinaryMissing,
            Subject:    binaryName,
            Severity:   SeverityHard,
            Resolution: ResolutionHumanRequired,
            Message:    fmt.Sprintf("%s is not installed in this environment", binaryName),
            Sticky:     true,
        })
    }
}
```

### 7.5 System Prompt Injection

**File**: `internal/agent/loop_pipeline_callbacks.go` — in `makeBuildMessages` or system prompt assembly:

```go
// After building system prompt, before returning messages:
if constraintSection := state.Tool.Constraints.ForSystemPrompt(); constraintSection != "" {
    // Append as system-level reminder
    msgs = append(msgs, providers.Message{
        Role:    "system",
        Content: constraintSection,
    })
}
```

### 7.6 Transition Check Method

**File**: `internal/pipeline/tool_stage.go`:

```go
func (s *ToolStage) checkConstraintTransition(state *RunState, c *Constraint) {
    if c == nil {
        return
    }
    switch {
    // P0: Hard constraint + affects everything → blocked
    case c.Severity == SeverityHard && c.Resolution == ResolutionHumanRequired:
        state.Turn.ArmCloseout(TurnCloseoutReason("constraint_" + string(c.Kind)))
        state.Turn.Phase = TurnPhaseNeedsHuman
        s.result = BreakLoop

    // P1: Needs human
    case c.Resolution == ResolutionHumanRequired:
        state.Turn.MissingPrereq = true

    // P2: Multiple consecutive blocks → partial
    case state.Tool.Constraints.consecutiveBlocked >= 2:
        state.Turn.ArmCloseout(TurnCloseoutReasonNoProgressLoop)
    }
}
```

---

## 8. File Map

| File | Status | Lines | Purpose |
|------|--------|-------|---------|
| `internal/pipeline/constraint.go` | **NEW** | ~80 | Constraint types, kinds, severity, resolution |
| `internal/pipeline/constraint_store.go` | **NEW** | ~120 | ConstraintStore with Check, Add, ForSystemPrompt |
| `internal/pipeline/novelty_tracker.go` | **NEW** | ~90 | NoveltyTracker with Record, CheckExactRepeat |
| `internal/pipeline/substates.go` | **MODIFY** | +3 | Add Constraints + Novelty to ToolState |
| `internal/pipeline/run_state.go` | **MODIFY** | +3 | Init ConstraintStore + NoveltyTracker |
| `internal/pipeline/tool_stage.go` | **MODIFY** | +25 | Pre-call check + transition check |
| `internal/agent/loop_pipeline_tool_callbacks.go` | **MODIFY** | +15 | Post-call novelty + constraint collection |
| `internal/agent/exec_probe_recovery.go` | **MODIFY** | ~-40, +20 | Replace streak heuristic with constraint emission |
| `internal/tools/subagent_spawn.go` | **MODIFY** | +10 | Emit CapacityExhausted constraint |
| Agent system prompt builder | **MODIFY** | +5 | Inject ForSystemPrompt() |

**Total new code**: ~290 LOC (3 files)
**Total modified code**: ~80 LOC across 6 files
**Total removed code**: ~40 LOC (exec_probe_recovery simplification)

---

## 9. Testing Contract

### Unit Tests

| Test | Verifies |
|------|----------|
| `TestConstraintStore_Add_SeverityPrecedence` | Higher severity wins on same key |
| `TestConstraintStore_Check_BinaryMissing` | `exec("git clone")` blocked when `binary_missing:git` |
| `TestConstraintStore_Check_SpawnBlocked` | `spawn` blocked when `capacity_exhausted:spawn.children` |
| `TestConstraintStore_Check_LowSignal` | `web_fetch(url)` blocked when `low_signal:url` |
| `TestConstraintStore_ClearNonSticky` | Soft constraints cleared per iteration |
| `TestConstraintStore_ForSystemPrompt` | Correct formatting with icons |
| `TestNovelty_ExactRepeat` | Same tool+args detected |
| `TestNovelty_ContentSimilarity` | Same result hash detected |
| `TestNovelty_DiminishingReturns` | Shrinking results detected |
| `TestTransition_HardHuman_ToNeedsHuman` | P1 transition fires |
| `TestTransition_TwoBlocks_ToPartial` | P2 transition fires |
| `TestTransition_SoftWithAlternatives_StaysRunning` | P3 stays running |

### Integration Scenarios (manual or e2e)

| Scenario | Expected behavior |
|----------|-------------------|
| Agent probes `git --version` → "not found" | Constraint emitted. Next `git clone` blocked pre-call. Agent explains missing prereq. |
| Agent spawns 5 children → hits limit | Constraint emitted. Next spawn blocked. Agent synthesizes locally or waits. |
| Agent fetches GitHub URL → HTML chrome | Constraint emitted on repeat. Agent tries API or summarizes from evidence. |
| Agent calls `exec("ls /tmp")` → success | No constraint. Normal flow. |
| Agent calls `exec("rm -rf /")` → policy block | `policy_blocked` constraint. Next similar command blocked. |

---

## 10. What This Spec Explicitly Excludes (Phase 2)

| Feature | Why excluded | When to revisit |
|---------|-------------|-----------------|
| ActionFamily taxonomy | Over-engineering for current scale | If >50 tools need coordinated gating |
| ResourceResolver | Tool-level fix is better | If >3 URL types need runtime classification |
| LLM-based observation scoring | Expensive, recursive | If deterministic novelty rules miss >20% of cases |
| Branch persistence to DB | Storage debt risk | If cross-session constraint carryover is needed |
| BranchPolicyEngine interface | Abstraction without proven need | If constraint matching logic exceeds 200 LOC |

---

## 11. Acceptance Criteria

CP-09 is complete when:

1. **Session A (git missing)**: After `git` discovered missing, zero subsequent `git` calls execute. Agent explains the blocker within 1 additional iteration.
2. **Session B (page loop)**: After 2 identical low-signal fetches, URL is constrained. Agent switches approach or answers from evidence.
3. **Session C (spawn quota)**: After first capacity error, zero subsequent `spawn` calls execute. Agent synthesizes locally.
4. **No regressions**: Existing test suite passes. No latency increase for unconstrained tool calls (Check is O(n) where n = active constraints, typically <10).
5. **Trace visibility**: Active constraints appear in trace metadata per iteration.
