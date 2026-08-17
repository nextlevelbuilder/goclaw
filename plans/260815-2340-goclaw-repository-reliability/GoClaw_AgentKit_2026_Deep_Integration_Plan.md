# GoClaw 2026 — AgentKit Deep Integration & Next-Gen Agent Runtime Plan

> Mục tiêu: nâng cấp `nextlevelbuilder/goclaw` thành một Agent Runtime mạnh hơn cho năm 2026, đồng thời tích hợp sâu tư duy và cấu trúc của AgentKit vào GoClaw.
>
> Repo nền tảng: `https://github.com/nextlevelbuilder/goclaw`
>
> AgentKit reference: `https://agentkit.best/docs`
>
> Command UX mục tiêu:
>
> ```text
> /gc:plan ...
> /gc:cook ...
> /gc:fix ...
> /gc:review ...
> /gc:ask ...
> /gc:research ...
> /gc:refactor ...
> /gc:test ...
> /gc:deploy ...
> ```

---

# 0. Vision

GoClaw không chỉ là chatbot có tools.

Đích đến là:

```text
GoClaw
├── Agent Runtime
├── AgentKit Runtime
├── Workflow Engine
├── Skill Engine
├── Subagent / Team Runtime
├── Memory + Knowledge
├── Model Router
├── Reliability Engine
├── Quality Gates
├── Checkpoint / Resume
├── Event & Trace System
└── Multi-tenant Control Plane
```

Mỗi agent execution phải trở thành một **durable run** có:

```text
Intent
→ Plan
→ Execute
→ Observe
→ Verify
→ Recover
→ Continue
→ Complete
```

Không được coi:

```text
HTTP response
WebSocket disconnect
LLM empty output
429
timeout
tool error
```

đồng nghĩa với:

```text
AGENT FAILED
```

---

# 1. AgentKit Integration Strategy

AgentKit hiện cung cấp CLI `ak`, kit, skills và các workflow-oriented commands như `/ak:cook`, `/ak:plan`, `/ak:fix`, `/ak:ask`; tài liệu cũng có portable target cho các coding agent khác, nhưng portable hiện chỉ được mô tả là bridge với khoảng 80–90% capability so với target native. Vì vậy GoClaw nên xây dựng **native GoClaw adapter/runtime**, thay vì chỉ shell-out tới `ak`.

Reference:

- `https://agentkit.best/docs`
- `https://docs.agentkit.best/`

## 1.1 Không làm

Không nên:

```text
/gc:plan
  ↓
exec("ak ...")
  ↓
parse stdout
```

Đây chỉ là wrapper.

## 1.2 Nên làm

```text
/gc:plan
    ↓
Command Parser
    ↓
Skill Resolver
    ↓
Workflow Resolver
    ↓
Agent Runtime
    ↓
Context Builder
    ↓
Planner
    ↓
Tool Executor
    ↓
Verifier
    ↓
Artifact Store
    ↓
Checkpoint
    ↓
Final Response
```

GoClaw phải hiểu native:

```text
Kit
Skill
Agent
Workflow
Hook
Artifact
Checkpoint
Quality Gate
Handoff
```

---

# 2. `/gc:` Command System

## 2.1 Command grammar

```text
/gc:<command> <input>
```

Ví dụ:

```text
/gc:plan thêm hệ thống cache Redis
/gc:fix sửa lỗi 429 khi gọi model
/gc:cook thêm tính năng export PDF
/gc:review kiểm tra PR này
/gc:research tìm nguyên nhân memory leak
```

Support flags:

```text
/gc:fix --hard ...
/gc:plan --deep ...
/gc:cook --safe ...
/gc:research --parallel ...
/gc:review --strict ...
```

## 2.2 Command aliases

```text
/gc:p  → /gc:plan
/gc:f  → /gc:fix
/gc:c  → /gc:cook
/gc:r  → /gc:review
```

Không bắt buộc enable aliases mặc định.

---

# 3. Core AgentKit Object Model

## 3.1 Kit

Kit là package cấp cao:

```yaml
name: engineer
version: 1
skills:
  - plan
  - cook
  - fix
  - review
  - research
  - test
  - refactor
```

GoClaw phải hỗ trợ:

```text
Built-in kits
Remote kits
Local kits
Tenant kits
Project kits
Private kits
```

## 3.2 Skill

Skill là capability có contract rõ ràng.

Ví dụ:

```text
skill: fix

input:
  issue

requires:
  tools:
    - shell
    - filesystem
    - search

outputs:
  - diagnosis
  - patch
  - tests
  - summary
```

Skill phải có:

```text
name
description
version
inputs
outputs
allowed_tools
preferred_models
fallback_models
workflow
quality_gates
permissions
```

## 3.3 Agent

Agent có:

```text
identity
role
model_policy
tool_policy
memory_policy
skill_policy
security_policy
budget_policy
```

Ví dụ:

```yaml
name: debugger
role: root-cause-analysis
preferred_models:
  - reasoning
fallback_models:
  - fast-code
skills:
  - fix
  - test
```

---

# 4. Native Skill Runtime

Mỗi `/gc:*` command phải resolve thành Skill.

```text
/gc:fix
     ↓
skill.fix
     ↓
workflow.fix
     ↓
agent.debugger
```

Skill engine cần:

```text
Skill Discovery
Skill Loading
Skill Versioning
Skill Dependency Resolution
Skill Permission Checking
Skill Execution
Skill Verification
Skill Caching
Skill Telemetry
```

---

# 5. Built-in GoClaw Engineer Kit

Tạo kit mặc định:

```text
go-claw-engineer/
```

## Commands

```text
/gc:plan
/gc:cook
/gc:fix
/gc:review
/gc:ask
/gc:research
/gc:refactor
/gc:test
/gc:debug
/gc:optimize
/gc:security
/gc:docs
/gc:architect
/gc:migrate
/gc:deploy
```

---

# 6. `/gc:plan`

Pipeline:

```text
Understand request
→ inspect repository
→ identify architecture
→ identify constraints
→ dependency analysis
→ risk analysis
→ implementation plan
→ verification plan
→ artifact
```

Output:

```text
plans/<timestamp>-<slug>.md
```

Plan phải chứa:

```text
Goal
Context
Current Architecture
Problem
Proposed Architecture
Files to Change
Migration
Risks
Test Plan
Rollback Plan
Acceptance Criteria
```

---

# 7. `/gc:cook`

`cook` tương đương implementation mode.

Pipeline:

```text
Read plan
→ create checkpoint
→ modify code
→ run tests
→ inspect failures
→ repair
→ quality gate
→ artifact
```

Không được coi:

```text
code generated
```

là:

```text
task completed
```

Task chỉ completed sau Verification.

---

# 8. `/gc:fix`

Fix phải có Root Cause Analysis.

```text
Reproduce
→ Collect evidence
→ Hypothesis
→ Test hypothesis
→ Root cause
→ Minimal fix
→ Regression test
→ Verification
```

Support modes:

```text
/gc:fix --fast
/gc:fix --deep
/gc:fix --hard
```

---

# 9. `/gc:review`

Review gồm:

```text
Correctness
Security
Performance
Concurrency
Reliability
Maintainability
API compatibility
Tests
Observability
```

Output severity:

```text
BLOCKER
CRITICAL
HIGH
MEDIUM
LOW
INFO
```

Có thể tự tạo:

```text
review-report.md
```

---

# 10. `/gc:research`

Research mode cho phép agent:

```text
search web
search repository
inspect documentation
compare implementations
collect evidence
build conclusion
```

Research phải lưu:

```text
sources
claims
confidence
unknowns
```

Để tránh:

```text
hallucinated certainty
```

---

# 11. `/gc:architect`

Agent Architect chuyên:

```text
system design
database design
service boundaries
queue architecture
scalability
fault tolerance
security
cost
```

Output:

```text
architecture.md
architecture-diagram
ADR
migration-plan
```

---

# 12. `/gc:research + plan + cook`

Cho phép chaining:

```text
/gc:research ...
→ /gc:plan
→ /gc:cook
→ /gc:test
→ /gc:review
```

Một execution có thể trở thành:

```text
Workflow DAG
```

Ví dụ:

```text
research
 ├── repo-analysis
 ├── web-research
 └── dependency-analysis
          ↓
         plan
          ↓
        cook
       /    \
    tests   review
       \    /
       quality-gate
          ↓
       complete
```

---

# 13. Workflow Engine

Workflow phải native.

```yaml
name: fix

steps:
  - analyze
  - reproduce
  - diagnose
  - patch
  - test
  - review
  - complete
```

Support:

```text
Sequential
Parallel
Conditional
Retry
Loop
Timeout
Human approval
Rollback
Compensation
```

---

# 14. Durable Agent Runs

Mỗi run:

```text
run_id
tenant_id
session_id
agent_id
skill_id
workflow_id
model_id
status
checkpoint
budget
started_at
updated_at
```

State:

```text
QUEUED
RUNNING
THINKING
WAITING_PROVIDER
WAITING_TOOL
RECOVERING
VERIFYING
COMPLETING
COMPLETED
FAILED
PAUSED
CANCELLED
```

---

# 15. Checkpoint / Resume

Checkpoint after important transitions:

```text
after_plan
after_tool
after_patch
after_test
after_review
before_external_side_effect
```

Resume:

```text
/gc:resume <run-id>
```

Hoặc:

```text
/gc:continue
```

Agent phải tiếp tục từ checkpoint cuối cùng thay vì chạy lại toàn bộ.

---

# 16. Agent Hibernation

Đây là feature rất đáng làm cho 2026.

Agent có thể:

```text
run
→ checkpoint
→ hibernate
→ wake
→ continue
```

Ví dụ task mất 30 phút:

```text
job waits for deployment
job waits for human approval
job waits for provider cooldown
```

Không giữ goroutine sống liên tục.

---

# 17. Event-Sourced Agent Runtime

Mỗi execution emit events:

```text
RunCreated
PlanStarted
LLMRequested
LLMResponded
ToolCalled
ToolReturned
CheckpointCreated
RecoveryStarted
VerifierStarted
VerifierPassed
RunCompleted
```

Lưu event.

Nhờ vậy có thể:

```text
replay
debug
audit
resume
analytics
```

---

# 18. Agent Time Travel

Feature rất “xịn”.

Cho phép debug:

```text
/gc:inspect <run-id>
/gc:replay <run-id>
/gc:fork <run-id> --from checkpoint-12
```

Tạo branch:

```text
Original
   ↓
checkpoint-12
   ├── model-A
   ├── model-B
   └── alternative strategy
```

Dùng để test model và strategy.

---

# 19. Model Router 2.0

Không dùng một model cố định cho mọi nhiệm vụ.

Router dựa trên:

```text
task complexity
reasoning requirement
tool calling reliability
latency
price
context size
provider health
historical success rate
```

Ví dụ:

```text
simple classification
→ cheap model

coding
→ coding model

architecture
→ reasoning model

review
→ independent reviewer model
```

---

# 20. Model Capability Profile

Mỗi model lưu:

```yaml
tool_calling: 0.91
reasoning: 0.95
coding: 0.93
structured_output: 0.97
long_context: 0.88
latency: 0.72
reliability: 0.91
```

GoClaw tự học từ telemetry.

---

# 21. Weak Model Resilience

Đây là phần bắt buộc vì model yếu dễ:

```text
empty output
bad tool call
wrong tool arguments
premature completion
loop
loss of context
```

Runtime phải có:

```text
Output Validator
Tool Schema Repair
Argument Repair
Continuation Detector
Loop Detector
Completion Verifier
Fallback Model
Strategy Switcher
```

Ví dụ:

```text
model says "done"
↓
verifier checks acceptance criteria
↓
criteria incomplete
↓
CONTINUE
```

---

# 22. Strategy Switching

Không chỉ đổi model.

Agent có thể đổi strategy:

```text
direct coding
→ planning first
→ decomposition
→ subagent
→ test-driven
→ research-first
→ alternative implementation
```

Ví dụ model yếu bị loop:

```text
Strategy A failed
↓
Strategy B
↓
smaller subtasks
↓
fresh context
```

---

# 23. Completion Verifier

Mọi important workflow phải có verifier.

Verifier kiểm tra:

```text
Acceptance Criteria
Expected Files
Expected Behavior
Tests
Build
Lint
Security
Regression
```

Không cho agent tự tuyên bố completion nếu verifier chưa pass.

---

# 24. Quality Gates

```text
Gate 1 — syntax
Gate 2 — typecheck
Gate 3 — unit tests
Gate 4 — integration tests
Gate 5 — security
Gate 6 — review
Gate 7 — acceptance
```

Configurable:

```yaml
quality_gate:
  required:
    - tests
    - build
  optional:
    - security
    - review
```

---

# 25. Self-Correction Engine

Agent có thể tự sửa:

```text
Failure
→ categorize
→ choose recovery policy
→ execute recovery
→ verify
```

Recovery classes:

```text
provider
tool
syntax
logic
context
permission
timeout
resource
dependency
```

---

# 26. Agent Teams 2.0

GoClaw đã có Agent Teams / orchestration. Phần mới nên biến team thành một runtime có contract rõ ràng.

Roles:

```text
Planner
Researcher
Coder
Tester
Reviewer
Security
Architect
Debugger
```

Workflow:

```text
Planner
   ↓
Researcher
   ↓
Coder
   ↓
Tester
   ↓
Reviewer
   ↓
Security
```

Không nhất thiết spawn tất cả agent.

Router quyết định:

```text
single-agent
pair-agent
team
```

---

# 27. Competitive Parallel Agents

Feature:

```text
Run 3 strategies in parallel
→ compare outputs
→ select best
```

Ví dụ:

```text
Agent A: simplest solution
Agent B: performance solution
Agent C: safest solution
```

Judge:

```text
correctness
performance
complexity
security
```

Sau đó merge.

---

# 28. Agent Jury

Với task quan trọng:

```text
implementation agent
+
independent reviewer agent
+
verification agent
```

Reviewer không được xem reasoning nội bộ của implementation agent như nguồn chân lý; reviewer phải kiểm tra artifact/code độc lập.

---

# 29. Context Engineering 2.0

Thay vì nhồi toàn bộ context.

Context builder chia:

```text
L0: current task
L1: relevant files
L2: project architecture
L3: memory
L4: knowledge
L5: history
```

Load dynamically.

Có:

```text
context budget
context relevance score
context compression
context deduplication
```

---

# 30. Context Snapshot

Mỗi checkpoint lưu:

```text
prompt snapshot
relevant files
tool state
memory state
variables
model configuration
workflow state
```

Cho phép reproduce execution.

---

# 31. Artifact-Centric Agent

Agent không chỉ trả text.

Artifact types:

```text
plan
patch
code
report
test-report
architecture
ADR
research
review
deployment-plan
```

Artifact metadata:

```text
artifact_id
run_id
version
author_agent
created_at
parent_artifact
checksum
status
```

---

# 32. Artifact Version Graph

Ví dụ:

```text
plan-v1
   ↓
plan-v2
   ↓
implementation-v1
   ├── review-v1
   └── test-v1
```

Có thể trace:

```text
requirement
→ plan
→ implementation
→ tests
→ review
```

---

# 33. Human-in-the-Loop

Support:

```text
approval
rejection
edit
choose strategy
grant permission
```

Command:

```text
/gc:approve <run-id>
/gc:reject <run-id>
/gc:pause <run-id>
/gc:resume <run-id>
```

---

# 34. Predictive Failure Detection

Runtime dự đoán run sắp lỗi bằng telemetry:

```text
tool retry count
token growth
latency
empty responses
loop frequency
provider health
```

Nếu risk cao:

```text
switch model
reduce scope
create checkpoint
spawn reviewer
```

---

# 35. Agent Budget Controller

Budget:

```text
token budget
time budget
tool budget
cost budget
retry budget
subagent budget
```

Ví dụ:

```yaml
budget:
  max_cost: 0.50
  max_duration: 15m
  max_tool_calls: 80
  max_subagents: 4
```

Runtime tự điều chỉnh.

---

# 36. Cost-Aware Execution

Không chỉ chọn model theo quality.

Tính:

```text
expected_success_probability
× expected_cost
× expected_latency
```

Router chọn strategy tối ưu.

---

# 37. Provider Pool

Provider health:

```text
healthy
degraded
rate_limited
cooldown
down
```

Theo:

```text
provider
endpoint
model
credential
region
```

429 của provider A không được làm chết task đang có thể chạy qua provider B.

---

# 38. Shared Cooldown

Khi một model/provider trả:

```text
429
```

các execution khác phải thấy cooldown.

Không để:

```text
goroutine A → 429
goroutine B → request
goroutine C → request
goroutine D → request
```

thành một storm.

---

# 39. Adaptive Retry

Retry không cố định.

```text
429 → Retry-After aware
5xx → exponential backoff
timeout → adaptive timeout
malformed output → repair
tool error → tool recovery
empty result → continuation
```

Có jitter.

Có global concurrency limits.

---

# 40. Streaming Recovery

Nếu stream chết:

```text
stream disconnect
↓
run remains RUNNING
↓
checkpoint
↓
resume/continue
↓
reconnect UI
```

UI không được hiển thị:

```text
Something went wrong
```

cho đến khi runtime kết luận run thực sự FAILED.

---

# 41. Better UX Status

Thay:

```text
Something went wrong.
```

bằng:

```text
⏳ Model đang giới hạn request — đang cooldown...
🔄 Đang thử provider dự phòng...
🧠 Agent vẫn đang xử lý...
🔧 Đang sửa tool call...
🧪 Đang verify kết quả...
✅ Hoàn tất.
```

UI phải lấy status từ Run State.

---

# 42. Agent Presence

Agent có trạng thái:

```text
🧠 Thinking
🔍 Researching
🛠 Working
⏳ Waiting
🔄 Recovering
🧪 Verifying
📦 Preparing result
```

Không mô phỏng giả; state phải đến từ runtime event.

---

# 43. Agent Memory Upgrade

GoClaw đã có Working / Episodic / Semantic memory.

Bổ sung:

```text
Procedural Memory
```

Lưu:

```text
how this task was solved
which strategy worked
which provider worked
which tools failed
```

Ví dụ:

```text
Project X + Go + PostgreSQL
→ previous fix required migration lock
```

Agent có thể học từ execution history.

---

# 44. Agent Experience Store

Lưu experience:

```text
task type
strategy
model
tools
success
cost
latency
failures
final solution
```

Lần sau router có thể:

```text
recommend strategy
```

---

# 45. Skill Marketplace Compatibility

Thiết kế Skill package:

```text
skill.yaml
SKILL.md
prompts/
scripts/
templates/
tests/
manifest.json
```

Support:

```text
install
update
pin version
verify checksum
rollback
```

Không chạy arbitrary skill trước khi permission check.

---

# 46. Secure Skill Sandbox

Skill có capability declaration:

```yaml
permissions:
  filesystem: project
  network: none
  shell: restricted
  secrets: none
```

Skill xin quyền ngoài contract:

```text
→ deny / human approval
```

---

# 47. Skill Dependency Graph

Ví dụ:

```text
cook
├── plan
├── filesystem
├── shell
└── test
```

Resolver phải detect:

```text
dependency conflict
version conflict
cycle
missing capability
```

---

# 48. Hook System

Hooks:

```text
before_run
after_run
before_llm
after_llm
before_tool
after_tool
before_checkpoint
after_checkpoint
before_complete
after_complete
on_error
on_rate_limit
```

Use cases:

```text
audit
security
logging
redaction
cost tracking
quality gates
notifications
```

---

# 49. Policy Engine

Policy quyết định:

```text
agent có được dùng tool?
agent có được spawn subagent?
agent có được network?
agent có được deploy?
```

Policy theo:

```text
tenant
user
agent
skill
workflow
tool
environment
risk level
```

---

# 50. Risk-Based Autonomy

Không phải task nào cũng cần approval.

```text
LOW
→ autonomous

MEDIUM
→ autonomous + logging

HIGH
→ approval

CRITICAL
→ explicit human approval
```

Ví dụ:

```text
read file → LOW
edit source → MEDIUM
delete database → HIGH
production deploy → CRITICAL
```

---

# 51. Sandboxed Execution

Đặc biệt cho:

```text
shell
browser
code execution
package install
git
deployment
```

Có:

```text
filesystem jail
network policy
CPU limit
memory limit
time limit
process limit
```

---

# 52. Git-Native Agent

Built-in:

```text
/gc:branch
/gc:commit
/gc:diff
/gc:review
/gc:changelog
```

Agent có thể:

```text
create worktree
implement
test
commit
prepare PR
```

Không push/deploy nếu policy chưa cho phép.

---

# 53. Auto Worktree Isolation

Mỗi coding run:

```text
main
 ↓
agent-worktree/<run-id>
```

Agent sửa code trong isolated worktree.

Sau khi verify:

```text
merge
or
discard
```

---

# 54. Change Impact Analyzer

Trước khi sửa:

```text
changed files
→ dependency graph
→ affected packages
→ affected tests
→ API consumers
```

Agent dự đoán blast radius.

---

# 55. Regression Intelligence

Sau mỗi fix:

```text
bug
→ root cause signature
→ regression test
→ store knowledge
```

Lần sau bug tương tự:

```text
match previous incident
→ suggest fix
```

---

# 56. Agent Observability

Mỗi run phải trace:

```text
run
 ├── llm span
 ├── tool span
 ├── memory span
 ├── model routing span
 ├── retry span
 ├── verification span
 └── checkpoint span
```

Metrics:

```text
success_rate
recovery_rate
tool_error_rate
model_error_rate
completion_accuracy
cost_per_success
time_to_success
```

---

# 57. Agent Reliability Score

Mỗi:

```text
model
provider
skill
tool
workflow
agent
```

có reliability score.

Ví dụ:

```text
Debugger Agent: 96.2%
Provider A: 98.1%
Provider B: 83.4%
Skill fix: 97.7%
```

Router dùng dữ liệu thực tế.

---

# 58. Agent Evaluation Harness

Tạo:

```text
evals/
├── coding/
├── debugging/
├── planning/
├── tool-use/
├── reasoning/
├── security/
└── regression/
```

Mỗi release GoClaw chạy benchmark.

Metrics:

```text
task success
tool success
hallucination
cost
latency
recovery
```

---

# 59. Model A/B Testing

Một task có thể chạy:

```text
model A
model B
```

so sánh:

```text
quality
cost
latency
failure
```

Dùng `/gc:fork`.

---

# 60. Agent Simulation Mode

Trước khi production:

```text
/gc:simulate ...
```

Runtime mô phỏng:

```text
provider failure
tool failure
timeout
429
malformed output
network loss
```

Dùng chaos testing.

---

# 61. Chaos Engineering cho Agent

Inject:

```text
429
500
timeout
empty stream
disconnect
malformed JSON
tool failure
slow tool
memory pressure
provider unavailable
```

Đảm bảo agent:

```text
recover
checkpoint
fallback
resume
```

---

# 62. Multi-Agent Negotiation

Các agent không chỉ delegate.

Hỗ trợ:

```text
proposal
counter-proposal
critique
vote
consensus
```

Ví dụ architecture:

```text
Architect A
Architect B
Architect C
      ↓
Consensus Agent
```

---

# 63. Dynamic Team Formation

Agent tự chọn team:

```text
simple bug
→ debugger only

medium feature
→ planner + coder + tester

large architecture
→ architect + researchers + coder + security + reviewer
```

Không spawn đội hình cố định.

---

# 64. Agent-to-Agent Contract

Handoff phải có schema:

```yaml
task
context
constraints
artifacts
acceptance_criteria
deadline
budget
```

Không giao tiếp bằng text tự do בלבד.

---

# 65. Long-Running Mission Mode

Feature cấp cao:

```text
/gc:mission ...
```

Ví dụ:

```text
/gc:mission
nâng cấp toàn bộ auth system
```

GoClaw tạo:

```text
Mission
 ├── goals
 ├── milestones
 ├── tasks
 ├── agents
 ├── artifacts
 ├── checkpoints
 └── acceptance criteria
```

Mission có thể kéo dài nhiều ngày.

---

# 66. Cron + Mission

Cho phép:

```text
schedule mission
```

Ví dụ:

```text
mỗi tối:
→ scan security
→ scan dependency
→ run tests
→ report
```

---

# 67. Autonomous Maintenance Agent

GoClaw có thể tự:

```text
scan stale dependencies
scan broken tests
scan security findings
scan performance regressions
```

Nhưng remediation phải theo risk policy.

---

# 68. Project Understanding Daemon

Background agent tạo:

```text
codebase map
architecture map
dependency graph
test map
docs index
```

Agent không phải scan toàn repo lại mỗi prompt.

Kết quả được cache/index.

---

# 69. Semantic Project Index

Index:

```text
symbol
file
function
class
dependency
API
config
test
documentation
```

Hybrid retrieval:

```text
keyword
semantic
graph
recency
```

---

# 70. Agent Context Cache

Cache reusable:

```text
system prompt
project architecture
tool schemas
skill definitions
common instructions
```

Nhưng cache phải version-aware.

---

# 71. Prompt Cache Awareness

Model router nên hiểu:

```text
cacheable prefix
dynamic suffix
```

Tách context:

```text
stable
dynamic
ephemeral
```

Giảm token cost và latency.

---

# 72. Structured Output First

Agent internal APIs ưu tiên:

```json
{
  "status": "continue",
  "reason": "...",
  "actions": [],
  "artifacts": [],
  "next_step": "..."
}
```

Text chỉ là presentation layer.

---

# 73. Tool Contract Enforcement

Tool:

```text
schema
validation
timeout
retry policy
idempotency
permissions
audit
```

Tool response có:

```text
success
error_class
retryable
side_effects
```

---

# 74. Idempotent Tool Execution

Mỗi side-effect tool có:

```text
idempotency_key
```

Ngăn:

```text
retry
→ duplicate payment
→ duplicate message
→ duplicate deployment
```

---

# 75. Human Approval Queue

Dashboard:

```text
Pending approvals
```

Hiển thị:

```text
why
risk
commands
files
diff
expected impact
```

User chọn:

```text
approve once
approve session
approve skill
deny
```

---

# 76. Agent Security Layer 2.0

Bổ sung:

```text
prompt injection detector
tool permission guard
secret redaction
egress policy
filesystem policy
supply-chain checks
skill signature verification
```

Không cho skill tùy ý tải executable.

---

# 77. Tenant-Level Agent Customization

Mỗi tenant có:

```text
custom kits
custom skills
custom commands
custom policies
custom models
custom workflows
```

Nhưng vẫn inheritance:

```text
Global Kit
  ↓
Tenant Kit
  ↓
Project Kit
  ↓
User Override
```

---

# 78. `/gc:` Namespace Resolution

Priority:

```text
project skill
→ tenant skill
→ built-in skill
```

Support explicit:

```text
/gc:engineer/fix
/gc:security/scan
```

---

# 79. Kit Versioning

Mỗi project pin:

```text
engineer-kit@1.4.2
```

Có lockfile:

```text
.goclaw/kit.lock
```

Tránh update làm workflow đột nhiên đổi behavior.

---

# 80. Kit Registry

Registry metadata:

```text
name
version
author
license
checksum
dependencies
permissions
compatibility
```

Không trust package chỉ vì package name.

---

# 81. AgentKit Compatibility Layer

Support import:

```text
AgentKit kit
→ GoClaw Kit
```

Mapping:

```text
AgentKit skill
→ GoClaw Skill

AgentKit workflow
→ GoClaw Workflow

AgentKit agent
→ GoClaw Agent

AgentKit hook
→ GoClaw Hook
```

Portable target output có thể dùng như import source, nhưng runtime thực thi native trong GoClaw.

---

# 82. AgentKit Sync

Command:

```text
/gc:kit install engineer
/gc:kit update engineer
/gc:kit list
/gc:kit inspect engineer
```

Có:

```text
version pin
checksum
rollback
dry-run
```

---

# 83. `/gc:doctor`

Diagnostic command:

```text
/gc:doctor
```

Check:

```text
models
providers
API keys
tools
skills
kits
sandbox
database
queues
memory
permissions
```

---

# 84. `/gc:status`

```text
/gc:status
```

Hiển thị:

```text
active runs
queued runs
provider health
model health
failed runs
pending approvals
running agents
```

---

# 85. `/gc:runs`

```text
/gc:runs
/gc:runs --failed
/gc:runs --running
```

---

# 86. `/gc:explain`

Feature rất đáng có.

```text
/gc:explain <run-id>
```

Không dump private chain-of-thought.

Thay vào đó trả:

```text
decision summary
tools used
artifacts created
failures
recoveries
verification
final reason
```

---

# 87. `/gc:why`

Ví dụ:

```text
/gc:why fallback
/gc:why waiting
/gc:why failed
```

Trả execution metadata có thể audit.

---

# 88. Agent Control Commands

```text
/gc:pause
/gc:resume
/gc:cancel
/gc:retry
/gc:fork
/gc:rollback
/gc:approve
```

---

# 89. Smart Retry

```text
/gc:retry
```

Không chạy lại y nguyên.

System phân tích:

```text
previous failure
```

và thay đổi:

```text
model
prompt strategy
context
tool ordering
workflow branch
```

---

# 90. Persistent Queue

Dùng queue durable cho long-running jobs.

Execution:

```text
API
→ Queue
→ Worker
→ Run State
→ Event Store
```

Worker crash:

```text
Queue
→ another worker
→ checkpoint
→ resume
```

---

# 91. Backpressure

Mỗi tenant/user:

```text
max concurrent runs
max expensive runs
max tool concurrency
```

Fair scheduler:

```text
tenant A cannot starve tenant B
```

---

# 92. Resource-Aware Scheduling

Scheduler xét:

```text
CPU
RAM
queue depth
provider capacity
token budget
```

Agent nặng không làm nghẽn toàn hệ thống.

---

# 93. Background Agent Workers

Tách worker:

```text
agent-worker
tool-worker
research-worker
index-worker
eval-worker
maintenance-worker
```

Nhưng giữ modular architecture; chỉ tách process khi workload cần.

---

# 94. Realtime Event Protocol

WebSocket/SSE chỉ là transport.

Canonical event:

```json
{
  "run_id": "...",
  "seq": 120,
  "type": "tool.started",
  "timestamp": "...",
  "payload": {}
}
```

Event có monotonic sequence.

Client reconnect:

```text
last_seq = 120
→ replay 121+
```

---

# 95. UI/Telegram UX

Khi chạy `/gc:cook`:

```text
🧠 Planning
✓ Repository analyzed

🛠 Implementing
↳ editing 4 files

🧪 Testing
↳ 23/23 passed

🔍 Reviewing
↳ 0 blockers

✅ Completed
```

Không spam hàng trăm message.

Có progress aggregation.

---

# 96. Agent Notification Policy

Notification levels:

```text
silent
important
verbose
debug
```

User có thể:

```text
/gc:mode compact
/gc:mode verbose
```

---

# 97. Agent Artifacts in Telegram/Web

Khi tạo artifact:

```text
📄 Plan
🧪 Test Report
🔍 Review
📦 Patch
```

User có thể mở đúng artifact thay vì đọc một block text dài.

---

# 98. Agent Learning Loop

Mỗi completed run:

```text
run
→ score
→ identify successful strategy
→ store experience
```

Nhưng learning phải có guardrails.

Agent không tự sửa policy/security rules.

---

# 99. Self-Evolution Guardrails

Đã có self-evolution direction trong GoClaw.

Bổ sung:

```text
observe
→ propose
→ evaluate
→ sandbox
→ benchmark
→ human/system approval
→ adopt
```

Không:

```text
observe
→ self-modify production
```

---

# 100. Agent Policies as Code

Ví dụ:

```yaml
policy:
  max_cost: 1.0
  allow_network: false
  allow_deploy: false
  require_review: true
```

Version-controlled.

---

# 101. Failure Taxonomy

Chuẩn hóa:

```text
PROVIDER_RATE_LIMIT
PROVIDER_UNAVAILABLE
PROVIDER_TIMEOUT
MODEL_EMPTY
MODEL_MALFORMED
MODEL_PREMATURE_COMPLETION
TOOL_TIMEOUT
TOOL_PERMISSION
TOOL_SCHEMA
TOOL_RUNTIME
CONTEXT_OVERFLOW
RESOURCE_LIMIT
POLICY_DENIED
VERIFICATION_FAILED
USER_CANCELLED
```

Không trả mọi thứ thành:

```text
Something went wrong
```

---

# 102. User-Facing Error Contract

API nên trả:

```json
{
  "status": "recovering",
  "code": "PROVIDER_RATE_LIMIT",
  "retryable": true,
  "message": "Model đang cooldown",
  "next_action": "fallback"
}
```

---

# 103. Error Recovery Matrix

Ví dụ:

```text
429
→ wait
→ provider fallback
→ model fallback

timeout
→ adaptive retry
→ checkpoint
→ continuation

malformed tool call
→ repair
→ retry
→ strategy switch

premature completion
→ verifier
→ continuation

tool failure
→ retry
→ alternate tool
→ subagent

context overflow
→ summarize
→ prune
→ continue
```

---

# 104. Acceptance Criteria

Project chỉ đạt milestone AgentKit Deep Integration khi:

```text
[ ] /gc: commands work natively
[ ] Skills are first-class objects
[ ] Workflows are executable DAGs
[ ] Kits can be installed/versioned
[ ] Checkpoint/resume works
[ ] Run state survives UI disconnect
[ ] Provider cooldown is shared
[ ] Weak-model recovery works
[ ] Completion verification works
[ ] Quality gates work
[ ] Agent handoff uses structured contracts
[ ] Artifacts are persisted
[ ] Event replay works
[ ] /gc:doctor works
[ ] /gc:status works
[ ] /gc:runs works
[ ] /gc:explain works
[ ] Security policies apply to skills
[ ] Tenant isolation is preserved
[ ] Evaluation harness is automated
```

---

# 105. Recommended Implementation Phases

## Phase 1 — `/gc:` Foundation

```text
Command parser
Skill registry
Built-in engineer kit
/gc:plan
/gc:fix
/gc:cook
/gc:review
```

## Phase 2 — Durable Runtime

```text
Run State
Checkpoint
Resume
Event Store
Event Replay
Streaming recovery
```

## Phase 3 — AgentKit Native Layer

```text
Kit
Skill
Agent
Workflow
Hook
Artifact
Import/sync
Version lock
```

## Phase 4 — Reliability

```text
Model router
provider pool
adaptive retry
fallback
weak-model resilience
completion verifier
quality gates
```

## Phase 5 — Multi-Agent

```text
subagents
dynamic teams
competitive execution
jury
handoff contracts
```

## Phase 6 — 2026 Advanced Agent Features

```text
hibernation
time travel
mission mode
predictive failure detection
agent experience store
self-improvement loop
simulation mode
chaos engineering
```

## Phase 7 — Enterprise

```text
tenant policies
RBAC
approval queue
audit
cost governance
skill registry
signed packages
observability
```

---

# 106. Suggested Repository Structure

```text
internal/
├── agent/
│   ├── runtime/
│   ├── state/
│   ├── checkpoint/
│   ├── recovery/
│   ├── verifier/
│   └── scheduler/
│
├── agentkit/
│   ├── kits/
│   ├── skills/
│   ├── agents/
│   ├── workflows/
│   ├── hooks/
│   ├── registry/
│   └── importer/
│
├── commands/
│   └── gc/
│
├── orchestration/
│   ├── teams/
│   ├── delegation/
│   ├── handoff/
│   └── consensus/
│
├── model/
│   ├── router/
│   ├── health/
│   ├── fallback/
│   └── capabilities/
│
├── reliability/
│   ├── retry/
│   ├── circuitbreaker/
│   ├── cooldown/
│   └── backpressure/
│
├── artifact/
├── workflow/
├── policy/
├── evaluation/
└── observability/
```

---

# 107. Core Data Model

```text
Agent
Skill
Kit
Workflow
WorkflowRun
RunEvent
Checkpoint
Artifact
Approval
Policy
ModelProfile
ProviderHealth
Experience
Evaluation
```

Relations:

```text
Kit
 └── Skill
      └── Workflow
           └── WorkflowRun
                ├── RunEvent
                ├── Checkpoint
                ├── Artifact
                └── Evaluation
```

---

# 108. Definition of Done

Một agent execution chỉ được coi là hoàn tất khi:

```text
intent understood
AND
required work executed
AND
tools succeeded
AND
acceptance criteria evaluated
AND
quality gates passed
AND
artifacts persisted
AND
run state = COMPLETED
```

Không dùng:

```text
LLM said "done"
```

làm điều kiện completion.

---

# 109. Final Target Architecture

```text
                           ┌─────────────────────┐
                           │     User / API      │
                           └──────────┬──────────┘
                                      │
                                  /gc:...
                                      │
                           ┌──────────▼──────────┐
                           │   Command Router    │
                           └──────────┬──────────┘
                                      │
                           ┌──────────▼──────────┐
                           │    Kit / Skill      │
                           │      Resolver       │
                           └──────────┬──────────┘
                                      │
                           ┌──────────▼──────────┐
                           │   Workflow Engine   │
                           └──────────┬──────────┘
                                      │
                  ┌───────────────────┼───────────────────┐
                  │                   │                   │
          ┌───────▼───────┐   ┌──────▼──────┐   ┌──────▼──────┐
          │ Agent Runtime │   │ Model Router │   │ Tool Runtime │
          └───────┬───────┘   └──────┬──────┘   └──────┬──────┘
                  │                  │                  │
                  └──────────────────┼──────────────────┘
                                     │
                           ┌─────────▼─────────┐
                           │ Recovery / Verify │
                           └─────────┬─────────┘
                                     │
                     ┌───────────────┼───────────────┐
                     │               │               │
                Checkpoint       Artifact        Event Store
                     │               │               │
                     └───────────────┼───────────────┘
                                     │
                              COMPLETED / RESUME
```

---

# 110. End Goal

GoClaw 2026 phải tiến hóa từ:

```text
LLM + Tools + Chat
```

thành:

```text
Durable Agent Runtime
+
AgentKit-native Engineering System
+
Multi-Agent Orchestrator
+
Reliable Model Runtime
+
Artifact / Workflow Platform
```

Và trải nghiệm người dùng cuối phải đơn giản:

```text
/gc:plan ...
/gc:cook ...
/gc:fix ...
/gc:review ...
```

phía sau GoClaw tự xử lý:

```text
planning
context engineering
model routing
tool execution
subagents
checkpoint
retry
fallback
verification
artifacts
memory
security
observability
```

Người dùng không cần quan tâm bên dưới đang dùng model nào, provider nào hay agent nào.

**Đó mới là mục tiêu của GoClaw + AgentKit: một command interface rất đơn giản ở phía trước, nhưng một agent runtime cực kỳ sâu và chịu lỗi ở phía sau.**
