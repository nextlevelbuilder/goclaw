# Phản biện: Branch Viability Control Design Review

**Date**: 2026-04-12
**Author**: Claude Opus 4.6 (critical review)
**Subject**: Phân tích thiết kế "Branch Viability Control" của Codex
**Verdict**: Chẩn đoán đúng, kê đơn quá liều

---

## 1. Tài liệu gốc đúng ở đâu

### 1.1 Ba production failures là thật và đau

| Session | Vấn đề | Hậu quả |
|---------|--------|---------|
| A | Agent biết `git` missing, vẫn thử `git clone` | 6-10 iterations lãng phí |
| B | Agent đọc lại HTML rác GitHub thay vì lấy README | Output cuối là chrome artifacts |
| C | Agent cố spawn sau khi hit 5/5 child limit | 3-5 spawn attempts vô nghĩa |

Đây không phải edge cases. Đây là bugs ảnh hưởng user experience trực tiếp.

### 1.2 Pattern chung được nhận diện chính xác

> "Runtime gets decisive evidence → evidence chỉ là text → LLM vẫn tự do chọn branch cũ → runtime dừng quá muộn"

Đúng. `turn_state.go` hiện tại set `MissingPrereq = true` bằng string matching (line 65-71)
nhưng boolean đó **không ngăn** LLM gọi tool ở iteration tiếp. Nó chỉ ảnh hưởng
closeout message khi turn đã kết thúc — quá muộn.

### 1.3 Nguyên tắc cốt lõi đúng

- "Runtime owns truth, model owns reasoning" — đúng
- "No-progress must be semantic, not just exact-repeat" — đúng
- "Closeout is not the planner" — đúng
- Phê phán 5 bad fixes (lower threshold, prompt warnings, string checks everywhere, ban tools, raw output as evidence) — đều đúng

### 1.4 exec_probe_recovery.go là bằng chứng cho gap

123 dòng heuristic giải Session A (4 probes + 2 misses → inject hint).
Nó works nhưng:
- Narrow: chỉ cho exec probes
- Reactive: phải chờ 4 probes mới trigger
- Non-generalizable: Session B và C cần heuristic riêng

Tài liệu đúng khi nói đây không scale.

---

## 2. Tài liệu gốc sai ở đâu

### 2.1 Over-engineering: 7 subsystems cho 3 bugs

Đề xuất tạo:

| Subsystem | Interface | Purpose |
|-----------|-----------|---------|
| CapabilitySnapshotter | `Seed() + UpdateFromObservation()` | Track binary availability |
| ResourceResolver | `Classify() + BuildArtifactPlan()` | URL → resource kind |
| CapacitySnapshotter | `Seed() + UpdateFromObservation()` | Track spawn slots |
| ObservationInterpreter | `Interpret()` | Tool result → typed facts |
| BranchPolicyEngine | `Evaluate()` | Family → open/blocked/exhausted |
| TurnReducer | `ApplyObservation() + ApplyBranchDecision()` | State transitions |
| ActionFamily registry | catalog + preconditions | Symbolic action taxonomy |

Đó là **7 subsystems, 6 interfaces, 14+ files, 6 sub-phases** (CP-09A→F).

So sánh: `exec_probe_recovery.go` hiện tại là 123 LOC, giải Session A.
Đề xuất mới là ~2000+ LOC — tăng **16x complexity** để giải thêm 2 cases.

**Complexity phải tỷ lệ thuận với bài toán.** 3 production bugs không justify 7 subsystems.

### 2.2 ActionFamily là symbolic planner trá hình

Tài liệu nói:

> "The purpose is not to create a symbolic planner."

Rồi ngay sau đó define:

```
repo_bootstrap_local
repo_inspect_remote
repo_delegate
market_research_parallel
spawn_more_workers
coordinator_local_synthesis
```

**Đây chính xác là symbolic planning.** Phân loại intent thành discrete families +
đặt preconditions lên mỗi family = STRIPS planning language dưới lớp vỏ Go.

Vấn đề: **LLM không suy nghĩ theo action families.** LLM suy nghĩ: "tool nào giải task hiện tại?"
Ép runtime classify mỗi turn thành một family:

- **Path A**: LLM declare intent → thêm latency + failure mode (declare sai family)
- **Path B**: Runtime infer từ tool call → string matching heuristic (mâu thuẫn với chính tài liệu phê phán heuristics)
- **Path C**: Hardcode mapping tool→family → brittle, mỗi tool mới phải update catalog

Không path nào clean.

### 2.3 ObservationInterpreter là string matching đội mũ

Tài liệu phê phán "case-specific string checks everywhere" là bad fix #3.

Rồi đề xuất:

```go
func Interpret(toolName string, args map[string]any, result *Result, currentFamily ActionFamily) []ObservationFact
```

Hỏi: function này detect `missing_prereq(binary=git)` bằng gì?

| Option | Method | Problem |
|--------|--------|---------|
| A | `strings.Contains(result, "not found")` | Chính xác là string matching mà tài liệu chê |
| B | LLM classify observation | Đắt, recursive (LLM classify output của LLM) |
| C | Tool trả structured error | **Đây mới là giải pháp thật** — nhưng nếu tool đã trả structured error thì KHÔNG CẦN ObservationInterpreter |

ObservationInterpreter không giải quyết vấn đề. Nó move string matching
từ `turn_state.go:56-72` sang file khác rồi đặt tên mới.

### 2.4 ResourceResolver nhầm tầng trách nhiệm

Session B (GitHub page loop) là **lỗi tool**, không phải lỗi pipeline.

`web_fetch` nhận `https://github.com/user/repo` → trả raw HTML bao gồm nav bar,
sign-in prompt, footer. Đó là vì tool fetch HTML thay vì dùng GitHub API.

| Fix | Layer | Đúng/Sai |
|-----|-------|----------|
| `web_fetch` detect GitHub URL → dùng API | Tool | **Đúng** — tool owns domain logic |
| Tạo `github_repo_read` tool chuyên biệt | Tool | **Đúng** — tool per domain |
| Thêm ResourceResolver vào pipeline | Pipeline | **Sai** — pipeline biết quá nhiều domain logic |

**Separation of concerns**: Pipeline quản lý flow. Tool quản lý domain.
URL classification thuộc về tool, không thuộc về pipeline.

### 2.5 CP-09 sẽ bị scope creep

8 checkpoints hiện tại đã ambitious. Thêm CP-09 với 6 sub-phases + 3 extensions = 17 work units tổng.

Production wisdom: **plan nhỏ ship nhanh, plan to chết trong design phase**.

CP-09 sẽ gặp một trong hai:
1. Implement nửa, bỏ dở → tech debt + 3 bugs vẫn unfixed
2. Implement đầy đủ nhưng mất 8-12 weeks → block tất cả features khác

---

## 3. Root cause thật sự

Ba failures chia sẻ MỘT root cause:

> **Tool trả error dạng free-text. Runtime parse text bằng string matching.
> Kết quả parse KHÔNG ảnh hưởng tool availability ở turn tiếp theo.**

| Session | Error (free-text) | Runtime biết gì | Runtime LÀM gì | Cần làm gì |
|---------|-------------------|-----------------|-----------------|-------------|
| A | `"sh: git: not found"` | `MissingPrereq = true` | Đợi closeout | **Block git commands ngay** |
| B | HTML 10KB rác | Không gì cụ thể | Đợi read-only budget | **Block re-fetch URL đó** |
| C | `"max children reached (5/5)"` | Không gì cụ thể | Đợi no-progress | **Block spawn ngay** |

Gap không phải "thiếu branch viability subsystem".
Gap là: **tool errors không structured + runtime không nhớ constraints sticky + runtime không block tool pre-call**.

---

## 4. Giải pháp đề xuất: Structured Constraints (~300 LOC)

### 4.1 Component 1: Structured Tool Errors

Thêm vào `tools.Result` (đã tồn tại):

```go
type ConstraintKind string

const (
    ConstraintBinaryMissing     ConstraintKind = "binary_missing"
    ConstraintCapacityExhausted ConstraintKind = "capacity_exhausted"
    ConstraintPolicyBlocked     ConstraintKind = "policy_blocked"
    ConstraintLowSignal         ConstraintKind = "low_signal"
    ConstraintResourceExhausted ConstraintKind = "resource_exhausted"
)

type Constraint struct {
    Kind    ConstraintKind
    Subject string  // "git", "spawn.children", "https://github.com/..."
    Message string  // human-readable
    Sticky  bool    // persists across iterations until explicitly cleared
}
```

**Mỗi tool emit constraint khi detect decisive failure.** Tool biết context
tốt nhất — không cần central ObservationInterpreter.

```go
// exec tool: "not found" → emit constraint
if strings.Contains(stderr, "not found") {
    result.Constraints = append(result.Constraints, Constraint{
        Kind:    ConstraintBinaryMissing,
        Subject: extractBinaryName(command),
        Message: fmt.Sprintf("%s is not installed", binaryName),
        Sticky:  true,
    })
}

// spawn tool: 5/5 → emit constraint
if childCount >= cfg.MaxChildrenPerAgent {
    result.Constraints = append(result.Constraints, Constraint{
        Kind:    ConstraintCapacityExhausted,
        Subject: "spawn.children",
        Message: fmt.Sprintf("child limit reached (%d/%d)", childCount, max),
        Sticky:  true,
    })
}

// web_fetch tool: low-signal → emit constraint
if isLowSignalHTML(body) {
    result.Constraints = append(result.Constraints, Constraint{
        Kind:    ConstraintLowSignal,
        Subject: url,
        Message: "page returned generic site chrome, not useful content",
        Sticky:  true,
    })
}
```

### 4.2 Component 2: ConstraintStore (trong RunState)

```go
type ConstraintStore struct {
    mu          sync.RWMutex
    constraints map[string]Constraint // key = kind:subject
}

func (cs *ConstraintStore) Add(c Constraint)                        { ... }
func (cs *ConstraintStore) Has(kind ConstraintKind, subject string) bool { ... }
func (cs *ConstraintStore) Remove(kind ConstraintKind, subject string) { ... }
func (cs *ConstraintStore) All() []Constraint                       { ... }

// ForSystemPrompt generates a section injected into every LLM call
func (cs *ConstraintStore) ForSystemPrompt() string {
    active := cs.All()
    if len(active) == 0 { return "" }

    var sb strings.Builder
    sb.WriteString("[Active environment constraints — do NOT retry these]\n")
    for _, c := range active {
        sb.WriteString(fmt.Sprintf("- %s: %s — %s\n", c.Kind, c.Subject, c.Message))
    }
    return sb.String()
}
```

**~60 lines.** Sticky constraints persist across iterations. Non-sticky cleared mỗi turn.

### 4.3 Component 3: Pre-call Check (trong ToolStage)

```go
func checkConstraints(store *ConstraintStore, tc providers.ToolCall) (blocked bool, reason string) {
    switch tc.Name {
    case "exec", "bash":
        cmd, _ := tc.Args["command"].(string)
        base := extractBaseCommand(cmd)
        if store.Has(ConstraintBinaryMissing, base) {
            return true, fmt.Sprintf("%s is not available in this environment", base)
        }
    case "spawn":
        if store.Has(ConstraintCapacityExhausted, "spawn.children") {
            return true, "subagent child limit already reached"
        }
    case "web_fetch":
        url, _ := tc.Args["url"].(string)
        if store.Has(ConstraintLowSignal, url) {
            return true, "previous fetch returned low-signal content — try a different approach"
        }
    }
    return false, ""
}
```

Khi blocked, inject message thay vì execute:

```
[System] Tool "exec" blocked: git is not available in this environment.
Do not retry — explain the blocker or choose an alternative approach.
```

### 4.4 Luồng hoạt động

```
Turn 1: Agent gọi exec("git --version")
  → exec tool trả error + Constraint{BinaryMissing, "git", sticky=true}
  → ConstraintStore.Add()
  → TurnState.RecordToolObservation() (existing, still works)

Turn 2: System prompt includes "[Active constraints] binary_missing: git"
  → LLM thấy constraint → (thường) không thử git nữa
  → NẾU vẫn cố gọi exec("git clone ..."):
    → ToolStage.checkConstraints() → BLOCKED pre-call
    → Inject: "[System] git is not available"
    → LLM buộc chọn alternative

Turn 3: Agent giải thích blocker hoặc chọn repo_inspect_remote
  → DONE — 0 wasted iterations thay vì 6-10
```

---

## 5. So sánh hai hướng

| Tiêu chí | Branch Viability (Codex) | Structured Constraints (đề xuất) |
|----------|--------------------------|--------------------------------|
| **Files mới** | 14+ files | 2-3 files |
| **LOC** | ~2000+ | ~300 |
| **New interfaces** | 6 | 0 |
| **Subsystems** | 7 | 1 (ConstraintStore) |
| **Phases to ship** | 6 (CP-09A→F) | 1 |
| **Time to production** | 8-12 weeks | 1-2 weeks |
| **Solves Session A** | Yes | Yes |
| **Solves Session B** | Yes (via ResourceResolver) | Yes (via tool-level fix) |
| **Solves Session C** | Yes | Yes |
| **Extensibility** | High (ở chi phí complexity cao) | Medium (thêm ConstraintKind) |
| **Risk of not shipping** | HIGH | LOW |
| **Cần symbolic planning?** | Yes (ActionFamily) | No |
| **Ai quyết định action?** | Runtime (BranchPolicyEngine) | LLM (với constraint visibility) |
| **Failure mode** | Over-constrains LLM | Under-constrains (fallback = status quo) |

---

## 6. Phản biện cụ thể 5 điểm thiết kế của tài liệu

### 6.1 "Capability Snapshot should be stored in run state"

**Đồng ý** — nhưng ConstraintStore IS capability snapshot, chỉ ở đúng granularity:

```
Codex:      CapabilityState { binary.git: missing, binary.gh: available, ... }
Đề xuất:    ConstraintStore { "binary_missing:git": Constraint{...} }
```

Cùng semantics, khác complexity. ConstraintStore là flat map, không cần
CapabilityKey/CapabilityStatus/CapabilityFact type hierarchy.

### 6.2 "Resource-aware retrieval for repo page loop"

**Không đồng ý** — đây là tool quality problem, không phải pipeline problem.

`web_fetch` tool nên:
1. Detect GitHub/GitLab repo URL → call API thay vì HTML
2. Detect low-signal HTML (sign-in chrome, nav bar ratio) → emit ConstraintLowSignal
3. Auto-extract README khi fetch repo root

Đây là 1 tool fix, ~50 LOC. Không cần ResourceResolver + ArtifactPlan + SignalScore subsystems.

### 6.3 "Subagent admission should become part of planning state"

**Đồng ý một phần** — nhưng ConstraintStore đã giải:

```go
// spawn tool khi hit limit:
result.Constraints = append(result.Constraints, Constraint{
    Kind:    ConstraintCapacityExhausted,
    Subject: "spawn.children",
    Sticky:  true,
})
```

System prompt turn tiếp: `[Active constraints] capacity_exhausted: spawn.children — 5/5`
Pre-call check: spawn blocked.

Không cần CapacitySnapshotter interface + SpawnAdmissionPolicy + CoordinatorFallbackMode.
LLM đủ smart để switch sang local synthesis khi thấy "spawn blocked".

### 6.4 "Observation must be typed, not inferred from prose"

**Đồng ý — nhưng typing nên ở tool level, không pipeline level.**

Tool biết context rõ nhất:
- `exec` tool biết "not found" = binary missing
- `spawn` tool biết 5/5 = capacity exhausted
- `web_fetch` biết sign-in chrome = low signal

Pipeline KHÔNG biết. Pipeline chỉ thấy `Result{ForLLM: "some text"}`.
Ép pipeline interpret tool output = wrong layer = ObservationInterpreter heuristic mess.

**Giải pháp đúng**: tool emit typed `Constraint`, pipeline store và enforce.
Typing ở source (tool), enforcement ở sink (pipeline). Clean separation.

### 6.5 "Branch-level decisions based on four kinds of state"

**Quá phức tạp.** Bốn state types (capability, resource, capacity, evidence novelty)
với interactions giữa chúng tạo combinatorial explosion.

ConstraintStore collapse tất cả thành MỘT abstraction: `Constraint{Kind, Subject, Sticky}`.
- `binary_missing:git` = capability state
- `low_signal:https://...` = resource state
- `capacity_exhausted:spawn.children` = capacity state
- Evidence novelty = solved by injecting constraints vào prompt → LLM adjusts naturally

Một abstraction thay bốn. Simpler is better khi both solve the same 3 bugs.

---

## 7. Những gì NÊN giữ từ tài liệu gốc

Không phải tài liệu hoàn toàn sai. Giữ lại:

| Idea | Giữ? | Dưới dạng gì |
|------|------|---------------|
| Typed observation thay vì raw text | **Yes** | `ConstraintKind` enum |
| Capacity awareness | **Yes** | `ConstraintCapacityExhausted` |
| Semantic no-progress | **Yes** | Constraints in system prompt → LLM knows |
| Trace visibility | **Yes** | Log constraints vào trace metadata |
| TurnState extensions | **Yes** | Add ConstraintStore to RunState |
| ActionFamily taxonomy | **No** | LLM self-routes based on constraints |
| ResourceResolver | **No** | Fix at tool level |
| BranchPolicyEngine | **No** | Simple pre-call check đủ |
| ObservationInterpreter | **No** | Tool-level typing thay thế |
| 6-phase rollout | **No** | Ship 1 phase, iterate |

---

## 8. Phân tích rủi ro

### Rủi ro hướng Branch Viability (Codex)

| Risk | Xác suất | Impact |
|------|----------|--------|
| Scope creep → never ships | HIGH | CRITICAL — 3 bugs vẫn unfixed |
| ActionFamily taxonomy explosion | MEDIUM | HIGH — mỗi use case mới = family mới |
| Over-constraining LLM (block creative paths) | MEDIUM | HIGH — runtime quyết thay LLM |
| Team không reason được 7 subsystems | HIGH | MEDIUM — debugging = archaeology |
| Design paralysis (debating interfaces) | MEDIUM | HIGH — 6 phases = 6 design reviews |

### Rủi ro hướng Structured Constraints (đề xuất)

| Risk | Xác suất | Impact |
|------|----------|--------|
| Coverage quá narrow (thiếu ConstraintKind) | MEDIUM | **LOW** — thêm enum value khi cần |
| Tool authors quên emit constraint | MEDIUM | **LOW** — graceful degradation (status quo) |
| String matching vẫn cần trong tools | HIGH | **LOW** — contained per-tool, không scattered |
| Cần evolve toward richer system later | LOW | **MEDIUM** — evolve từ working simple > unshipped complex |
| Constraint stale (binary installed sau khi missing) | LOW | **LOW** — add TTL hoặc explicit clear |

**Key asymmetry**: Rủi ro lớn nhất của Codex là "never ships" (CRITICAL).
Rủi ro lớn nhất của đề xuất là "not expressive enough" (MEDIUM, fixable by adding ConstraintKind).

---

## 9. Implementation Plan

### Tuần 1 (3-4 ngày)

**Day 1-2**: ConstraintStore + types

```
internal/pipeline/constraint_store.go    (~80 LOC)
  - ConstraintKind enum (5 values)
  - Constraint struct
  - ConstraintStore (thread-safe map)
  - ForSystemPrompt() → injectable string
```

**Day 2-3**: Tool-level constraint emission

```
internal/tools/subagent_spawn.go         (+15 LOC, emit CapacityExhausted)
internal/agent/exec_probe_recovery.go    (+20 LOC, emit BinaryMissing — REPLACES current heuristic)
internal/tools/web_fetch.go (or equiv)   (+30 LOC, detect low-signal HTML)
```

**Day 3-4**: Pipeline integration

```
internal/pipeline/substates.go           (+1 line: Constraints *ConstraintStore in ToolState)
internal/pipeline/tool_stage.go          (+30 LOC: pre-call check + constraint collection)
internal/agent/loop_pipeline_adapter.go  (+5 LOC: init ConstraintStore)
Context/system prompt injection           (+10 LOC: inject ForSystemPrompt())
```

### Tuần 2 (2-3 ngày)

**Day 5-6**: Tests + trace logging

```
internal/pipeline/constraint_store_test.go
internal/tools/exec_constraint_test.go
Trace metadata: log active constraints per turn
```

**Day 6-7**: Production canary + observe

Ship. Monitor 3 failure patterns. Measure:
- Session A: does git retry drop to 0?
- Session B: does page re-fetch drop?
- Session C: does spawn retry drop to 0?

### Nếu cần mở rộng sau (tuần 3+):

- Thêm `ConstraintKind` mới cho patterns mới
- Thêm TTL cho constraints (binary có thể được install giữa session)
- Thêm constraint clear mechanism (user says "I installed git")
- **Chỉ** nếu production data cho thấy cần → evolve toward richer system

---

## 10. Đối chiếu với code hiện tại

Giải pháp này **extend** chứ không **replace** code hiện tại:

| Existing | Status | Action |
|----------|--------|--------|
| `turn_state.go` — TurnPhase/CloseoutReason | **Keep** | Constraints feed INTO TurnState |
| `turn_state.go` — MissingPrereq/BlockedByPolicy | **Keep** | ConstraintStore is richer version |
| `exec_probe_recovery.go` — 4-probe heuristic | **Evolve** | Replace streak counting with Constraint emission |
| `subagent_spawn.go:64` — error message | **Extend** | Add Constraint to Result alongside error |
| `tool_stage.go` — exit conditions | **Extend** | Add pre-call constraint check |
| ToolStage parallel execution | **Keep** | Constraints orthogonal to concurrency |
| PruneStage/ThinkStage | **Untouched** | Constraints are tool-layer concern |

**Zero breaking changes.** Constraints are additive. Tools that don't emit constraints
behave exactly as today. Pipeline without ConstraintStore = status quo.

---

## 11. Tóm tắt

### Chẩn đoán

Codex đúng: GoClaw cần runtime nhớ decisive failures và ngăn LLM retry branch chết.

### Đơn thuốc sai

7 subsystems, 6 interfaces, 14 files, 6 phases → over-engineering.
ActionFamily = symbolic planner trá hình. ObservationInterpreter = string matching đội mũ.
ResourceResolver = tool logic nhầm vào pipeline layer.

### Đơn thuốc đúng

3 components, ~300 LOC, 1-2 weeks:
1. **Structured Constraints** — tool emit typed errors (tool biết context tốt nhất)
2. **ConstraintStore** — runtime nhớ sticky (flat map, thread-safe)
3. **Pre-call check** — runtime block tool khi constraint active (simple switch/case)

Inject active constraints vào system prompt → LLM tự adjust.
Ship. Observe. Iterate. Chỉ evolve khi production data yêu cầu.

### Một dòng

> **Tool nào gây lỗi, tool đó phải nói rõ lỗi gì. Runtime nhớ và block.
> Không cần symbolic planner giữa tool và runtime.**
