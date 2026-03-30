# Failure Recovery Playbooks

Reference document for Straw Hat team failure handling procedures.

## 3-Strike Circuit Breaker

```
Task fails (attempt 1)
  → Preserve output for diagnosis
  → Auto-retry with incremented attempt_count
  → Assigned agent refines brief based on failure output

Task fails (attempt 2)
  → Preserve output
  → Auto-retry with escalation note to lead
  → Lead notified: "Task #N failed twice, reviewing on attempt 3"

Task fails (attempt 3)
  → STOP auto-retrying
  → Mark task as `blocked`
  → Post comment with all 3 attempt outputs
  → Lead must manually review and decide:
    a) Restructure the brief and retry
    b) Split into smaller tasks
    c) Escalate to user
    d) Cancel the task
```

Each retry brief should include:
```json
{
  "attempt_count": 2,
  "previous_error": "description of what failed",
  "refined_approach": "what to do differently this time"
}
```

## Worker Offline Protocol

```
Worker heartbeat missed for 2 intervals (60s)
  → VPS marks worker as offline
  → Active task → mark as stale

Worker offline + active task:
  → Lead notified: "Worker {id} offline, task #N needs reassignment"
  → Lead explicitly assigns to another worker or defers
  → If worker comes back → it must NOT re-claim reassigned tasks
    (check task status before attempting work)

Worker offline + no active task:
  → Pending tasks queue normally
  → Lead monitors and notifies user if tasks pile up
```

## User Non-Response Timeout

For tasks that need user clarification:

```
T+0h:  Send clarification request
T+1h:  Reminder 1 — "Still waiting for your input on task #N"
T+4h:  Reminder 2 — "Task #N blocked pending your response"
T+24h: Reminder 3 — "Final reminder: task #N needs your input"
T+48h: Escalate — Mark task as "deferred — user non-responsive"
        Lead decides: auto-close, extend, or reassign
```

## Circular Dependency Detection

Before creating any dependency link:

```
Check: Does target task already depend (transitively) on source task?

If Task A blocks Task B:
  → Walk Task A's blockedBy chain recursively
  → If Task B appears anywhere → CIRCULAR DEPENDENCY DETECTED
  → Reject the link
  → Notify lead with the full dependency chain

Resolution:
  → Lead restructures by:
    a) Breaking the cycle by splitting one task
    b) Reordering task dependencies
    c) Merging the circular tasks into one
```

## Timeout-Specific Recovery

When a task times out (distinct from failure):

```
Timeout detected:
  → Kill Claude Code process tree
  → Capture partial output
  → Post fail with reason: "timeout after {N}s"

Review partial output:
  → If progress was made → retry with narrower scope
  → If stuck at start → likely wrong approach, redesign brief
  → If output shows infinite loop → fix the prompt constraints

Timeout budgets by job type:
  implement: 1800s (30 min)
  debug:      900s (15 min)
  test:       900s (15 min)
  review:     600s (10 min)
```

## Blocker Taxonomy

Every blocker must be categorized:

| Type | Meaning | Resolution |
|------|---------|------------|
| `technical` | Code error, build failure, test failure | Retry with fix |
| `user_response` | Waiting on user input/decision | Reminder escalation |
| `worker_offline` | Worker unreachable | Reassignment |
| `timeout` | Worker timed out | Retry with narrower scope |
| `dependency` | Blocked by another task | Wait or restructure |

## Handoff Contract

### Agent → Worker (Brief Fields)

| Field | Required | Description |
|-------|----------|-------------|
| `execution_target` | yes | Always `windows-local` |
| `repo_key` | yes | Repository identifier (e.g., `goclaw`) |
| `job_type` | yes | `implement`, `debug`, `test`, `review` |
| `brief_markdown` | yes | Prompt for Claude Code |
| `files_of_interest` | no | Paths to focus on |
| `commands_to_run` | no | Verification commands |
| `acceptance_criteria` | no | Success conditions |
| `max_runtime_seconds` | yes | Timeout per job type |
| `attempt_count` | no | Which retry this is (1, 2, 3) |
| `dependency_task_ids` | no | Tasks this depends on |

### Worker → Agent (Result Fields)

```json
{
  "status": "pass | fail | blocker",
  "summary": "what was done (1-3 paragraphs)",
  "changed_files": ["path/to/file.go"],
  "commands_executed": ["go build ./..."],
  "test_results": "stdout summary (truncated to 4000 chars)",
  "branch": "task/{N}",
  "blocker_reason": "optional — only if status != pass",
  "duration_seconds": 120,
  "timed_out": false,
  "attempt_number": 1
}
```

Skills should emit structured result summaries, not raw diffs.
