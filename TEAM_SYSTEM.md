# Team System — Architecture & Design

GoClaw's Team System gồm 3 subsystem tích hợp chặt: **Task Management**, **Shared Workspace**, và **Delegation Engine**. Cả 3 hoạt động qua tool layer (LLM gọi), được bổ trợ bởi system-level guardrails (auto followup, stale recovery, advisory lock) để giảm phụ thuộc vào LLM nhớ đúng.

---

## 1. Tổng quan kiến trúc

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ User (Telegram / Discord / WS Dashboard / ...)                             │
└──────────────────────────────────┬──────────────────────────────────────────┘
                                   │ inbound message
                                   ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ gateway_consumer.go                                                         │
│  ├─ auto-clear followup khi user reply                                      │
│  ├─ route → agent loop → scheduler                                          │
│  └─ auto-set followup khi lead reply                                        │
└──────────────────────┬────────────────────────────────┬─────────────────────┘
                       │                                │
                       ▼                                ▼
            ┌──────────────────┐              ┌──────────────────────┐
            │ Agent Loop       │              │ TaskTicker (5 min)   │
            │ (think→act→obs)  │              │  ├─ processFollowups │
            │                  │              │  ├─ recoverStaleTasks│
            │ Tools available: │              │  ├─ dispatchPending  │
            │  team_tasks      │              │  └─ notifyLead      │
            │  workspace_write │              └──────────────────────┘
            │  workspace_read  │
            │  team_message    │
            │  delegate        │
            └──────┬───────────┘
                   │ tool calls
                   ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ TeamToolManager                                                             │
│  ├─ Team cache (5 min TTL, invalidate on mutation)                          │
│  ├─ RBAC: resolveTeamRole(agentID) → lead / member / reviewer             │
│  ├─ Escalation policy: checkEscalation(team, action)                       │
│  ├─ Settings parser: followup interval, max, quota, access control         │
│  └─ Event broadcast: WS events → UI realtime update                        │
└──────────────────────────────────────────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ PostgreSQL                                                                  │
│  ├─ agent_teams (CRUD, settings JSONB)                                      │
│  ├─ agent_team_members (lead / member / reviewer)                           │
│  ├─ team_tasks (+ comments, events, attachments)                            │
│  ├─ team_workspace_files (+ versions, comments)                             │
│  ├─ team_messages (mailbox)                                                │
│  ├─ team_delegation_history (audit)                                         │
│  └─ team_handoff_routes (routing override)                                 │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Key files

| File | Vai trò |
|------|---------|
| `internal/tools/team_tasks_tool.go` | Task CRUD + 14 actions |
| `internal/tools/workspace_tool_write.go` | File write, versioning, templates |
| `internal/tools/workspace_tool_read.go` | File read, delete, pin, tag, comments |
| `internal/tools/team_tool_manager.go` | Shared backend, cache, RBAC, escalation |
| `internal/tools/delegate_prep.go` | Auto-task creation, dependency injection |
| `internal/tools/delegate_state.go` | Auto-complete/fail, history persistence |
| `internal/tools/delegate_policy.go` | Access control, escalation gates |
| `internal/tools/team_message_tool.go` | Send/broadcast/read messages |
| `internal/tasks/task_ticker.go` | Recovery, re-dispatch, followup ticker |
| `internal/store/pg/teams_tasks.go` | Task SQL, lock management, unblock logic |
| `internal/store/pg/teams_workspace.go` | File upsert (advisory lock), versioning |
| `cmd/gateway_consumer.go` | Auto followup guardrails |

---

## 2. Task System

Khi team có nhiều agents, lead cần chia việc và track ai đang làm gì. Không có task system thì:
- Lead delegate xong không biết member đang ở bước nào, kẹt ở đâu
- Agent crash giữa chừng → công việc mất, không ai biết để retry
- Human không có visibility: task nào pending, ai own, kết quả ra sao
- Dependencies giữa tasks (task B chờ task A xong) không có cách enforce

Task system giải quyết bằng **shared task list với locking, dependency, progress tracking, và auto-recovery**. Mỗi delegation tự động tạo task record — đảm bảo mọi công việc đều được track kể cả khi LLM quên. Khi agent crash, ticker tự detect lock expire và reset task về pending để agent khác nhận lại.

### 2.1 Data model

```sql
-- Base: migration 003, v2: migration 018, followup: migration 019
team_tasks (
    -- Identity
    id                  UUID PRIMARY KEY,
    team_id             UUID NOT NULL,
    task_number         INT,              -- sequential per team
    identifier          VARCHAR(20),      -- "DEV-42" human-readable

    -- Content
    subject             VARCHAR(500) NOT NULL,
    description         TEXT,
    task_type           VARCHAR(30) DEFAULT 'general',  -- general|delegation|escalation|message
    priority            INT DEFAULT 0,                   -- higher = more important
    result              TEXT,                             -- completion result / failure reason

    -- Ownership
    owner_agent_id      UUID,             -- agent currently working on it
    created_by_agent_id UUID,             -- which agent created it
    user_id             VARCHAR(255),     -- human who triggered creation
    assignee_user_id    TEXT,             -- human assignee (for human tasks)

    -- Dependencies
    blocked_by          UUID[],           -- task IDs that must complete first
    parent_id           UUID,             -- subtask reference

    -- Scope (origin conversation)
    channel             VARCHAR(50),      -- telegram, discord, ws, ...
    chat_id             VARCHAR(255),     -- specific chat/group

    -- Execution locking
    locked_at           TIMESTAMPTZ,      -- when claimed
    lock_expires_at     TIMESTAMPTZ,      -- 30 min after claim (heartbeat renews)

    -- Progress
    progress_percent    INT DEFAULT 0,    -- 0-100
    progress_step       TEXT,             -- "Analyzing data", "Writing report"

    -- Follow-up reminders
    followup_at         TIMESTAMPTZ,      -- next reminder time
    followup_count      INT DEFAULT 0,    -- reminders already sent
    followup_max        INT DEFAULT 0,    -- max reminders (0 = unlimited)
    followup_message    TEXT,             -- reminder content
    followup_channel    VARCHAR(60),      -- where to send reminder
    followup_chat_id    VARCHAR(255),     -- target chat

    -- Status
    status              VARCHAR(20) DEFAULT 'pending',
    -- pending → in_progress → in_review → completed
    --                                    → cancelled
    --                                    → failed
    -- blocked (waiting on dependencies)

    -- Metadata
    metadata            JSONB,
    created_at          TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ
)

-- Supplementary tables
team_task_comments    (id, task_id, agent_id, user_id, content, created_at)
team_task_events      (id, task_id, event_type, actor_type, actor_id, data, created_at)
team_task_attachments (id, task_id, file_id, added_by, created_at)  -- links workspace files
```

### 2.2 Status flow

```
                              ┌──────────────────────────────────────┐
                              │          ┌──────────┐                │
                              │          │ blocked  │◄──── blocked_by│
                              │          └────┬─────┘   dependency   │
                              │               │ unblock              │
                              │               ▼                      │
     ┌──────────┐   claim   ┌─┴───────────┐  review  ┌───────────┐  │
     │ pending  │──────────►│ in_progress │─────────►│ in_review │  │
     └──────────┘           └──────┬──────┘          └─────┬─────┘  │
                                   │                   ┌───┴───┐    │
                              complete/fail       approve   reject  │
                                   │                │         │     │
                              ┌────┴────┐     ┌─────┴──┐ ┌────┴───┐│
                              │completed│     │completed│ │cancelled││
                              │ / failed│     └────────┘ └────────┘│
                              └─────────┘                          │
                                                                   │
                              cancel (lead only) ──────────────────┘
                              → cancelled, unblocks dependents
```

### 2.3 Tool actions (team_tasks)

| Action | Quyền | Mô tả |
|--------|-------|-------|
| `list` | All (user-scoped ngoài delegate channel) | Active tasks, filter by status/scope |
| `get` | All | Chi tiết + comments + events + attachments |
| `create` | Lead | Tạo task, auto-generate identifier |
| `claim` | Member/Lead | Atomic pending→in_progress, set lock 30min |
| `complete` | Owner | Kết thúc task, auto-unblock dependents |
| `cancel` | Lead | Hủy task, unblock dependents |
| `search` | All | Full-text search subject + description |
| `review` | Owner | Submit for human review |
| `comment` | Owner | Add comment (max 10K chars) |
| `progress` | Owner | Update percent + step description, renew lock |
| `update` | Lead | Edit subject/description |
| `attach` | Member/Lead | Link workspace file to task |
| `await_reply` | Lead/Owner | Set followup reminder (custom delay/max/message) |
| `clear_followup` | Lead/Owner | Cancel followup reminders |

### 2.4 Follow-up reminder system

Khi lead agent hỏi user câu hỏi (ví dụ: "Anh muốn chọn phương án nào?"), user có thể không trả lời ngay — bận, quên, hoặc đọc rồi lướt qua. Không có follow-up thì task đó sẽ treo mãi ở in_progress, không ai nhắc.

Follow-up giải quyết bằng cách **tự động gửi reminder qua channel** sau N phút nếu user chưa reply. Khi user reply → auto-clear. Vòng lặp: agent hỏi → set followup → chờ → reminder → user reply → clear → agent tiếp tục.

Hệ thống hoạt động ở 2 layer:

**LLM layer** — agent tự biết mình vừa hỏi gì (vì nó sinh ra câu hỏi), gọi `await_reply` với message/delay custom. System prompt hướng dẫn: "Khi bạn đặt câu hỏi cho user và cần chờ trả lời, gọi `await_reply`." Vấn đề: model nhỏ quên gọi tool → không có followup → task treo.

**System layer** — không phụ thuộc LLM nhớ, dùng heuristic đơn giản:
- Agent vừa reply user + có in_progress tasks → **khả năng cao đang chờ user phản hồi** → auto-set followup
- User gửi message vào channel → **đã trả lời** → auto-clear tất cả followup cho scope đó
- Nếu LLM đã gọi `await_reply` trước (với message riêng, delay riêng) → system không ghi đè

System không cần phân biệt nội dung input — chỉ dựa vào pattern "agent nói + có task = chờ user" và "user nói = đã trả lời". LLM layer cho phép customize chính xác hơn, system layer là safety net.

#### Auto followup guardrails chi tiết

**Auto-clear khi user reply** (`gateway_consumer.go`):
- User gửi message vào real channel (không phải system/delegate/dashboard)
- System auto-clear tất cả followup cho scope `(channel, chat_id)` đó
- Fire-and-forget goroutine, không block message processing

**Auto-set khi lead agent reply** (`gateway_consumer.go`):
- Lead agent trả lời user qua real channel
- Resolve agent → team → kiểm tra là lead agent
- Set followup trên tất cả in_progress tasks chưa có followup (`followup_at IS NULL`)
- Dùng team settings cho interval + max
- Followup message = truncated last line of agent response
- Nếu LLM đã gọi `await_reply` trước → không ghi đè (respect `followup_at IS NULL`)

### 2.5 Task ticker (background, 5 phút/lần)

Thứ tự xử lý mỗi tick cho mỗi team:

1. **Process followups** — query tasks có `followup_at <= NOW()` AND `status = 'in_progress'`:
   - Gửi reminder qua outbound bus: `"Reminder (N/M): {message}"`
   - Increment count, schedule next hoặc stop nếu đạt max
   - Cooldown: 5 phút per task

2. **Recover stale tasks** — tasks có `lock_expires_at < NOW()`:
   - Reset status → pending, clear owner + lock + followup
   - Startup: force reset ALL in_progress (assume stale)

3. **Re-dispatch** — pending tasks có assigned owner:
   - Gửi system message vào agent session
   - Cooldown: 10 phút per task

4. **Notify lead** — unassigned pending tasks + idle agents available:
   - Gửi summary cho lead: "N unassigned tasks, M available agents"
   - Cooldown: 30 phút per team

### 2.6 Execution locking

```
ClaimTask → locked_at = now(), lock_expires_at = now() + 30min
    │
    ├─ progress update → renew lock_expires_at (heartbeat)
    │
    ├─ complete/fail/cancel → clear lock
    │
    └─ lock_expires_at < now() → RecoverStaleTasks reset về pending
```

Lock 30 phút mặc định. `UpdateTaskProgress` renew lock — agent đang hoạt động sẽ không bị recover. Nếu agent crash/timeout → lock expire → ticker auto-recover.

---

## 3. Workspace System

Agents giao tiếp qua message chỉ truyền được text ngắn. Khi output lớn (report, data, code) thì message không đủ — delegation result bị truncate, mất context khi truyền giữa agents, và human không có chỗ review deliverables.

Workspace giải quyết bằng cách cung cấp **shared file system cho team agents** — nơi agents đọc/ghi files chung trong cùng scope. Ví dụ:

1. Lead delegate "research market data" cho researcher
2. Researcher viết `market-report.md` vào workspace, tag `deliverable`
3. Lead delegate "write summary" cho writer → writer đọc `market-report.md` (auto-injected vào context)
4. Writer viết `summary.md`, attach vào task
5. Human mở dashboard → thấy deliverables, review, approve

Nói ngắn gọn: **workspace = shared disk cho agents collaborate qua files thay vì chỉ qua messages**.

### 3.1 Data model

```sql
team_workspace_files (
    id          UUID PRIMARY KEY,
    team_id     UUID NOT NULL,
    channel     VARCHAR(60) NOT NULL,
    chat_id     VARCHAR(255) NOT NULL,
    file_name   VARCHAR(255) NOT NULL,
    mime_type   VARCHAR(100),
    file_path   TEXT NOT NULL,              -- disk path
    size_bytes  BIGINT DEFAULT 0,
    uploaded_by UUID NOT NULL,              -- agent
    task_id     UUID,                       -- optional link to task
    pinned      BOOLEAN DEFAULT FALSE,
    tags        TEXT[],                     -- deliverable, handoff, reference, draft
    metadata    JSONB,
    archived_at TIMESTAMPTZ,               -- soft-delete on task completion
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ,
    UNIQUE(team_id, channel, chat_id, file_name)
)

team_workspace_file_versions (
    id          UUID PRIMARY KEY,
    file_id     UUID NOT NULL,
    version     INT NOT NULL,
    file_path   TEXT NOT NULL,
    size_bytes  BIGINT,
    uploaded_by UUID NOT NULL,
    created_at  TIMESTAMPTZ,
    UNIQUE(file_id, version)
)

team_workspace_comments (
    id          UUID PRIMARY KEY,
    file_id     UUID NOT NULL,
    agent_id    UUID NOT NULL,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ
)
```

### 3.2 Tool actions

**Workspace Write (`workspace_write`):**

| Action | Quyền | Mô tả |
|--------|-------|-------|
| `write` | Member/Lead | Ghi 1-20 files, auto-version, advisory lock |
| `set_template` | Lead (escalation) | Define templates auto-seed vào scope mới |

**Workspace Read (`workspace_read`):**

| Action | Quyền | Mô tả |
|--------|-------|-------|
| `list` | All | List files + quota info + pin/tag status |
| `read` | All | Content (text) hoặc metadata (binary), support version |
| `delete` | Owner / Lead (escalation) | Xóa file, lead xóa file người khác cần escalation |
| `pin` | Lead (escalation) | Mark/unmark important files |
| `tag` | Lead (escalation) | Set tags: deliverable, handoff, reference, draft |
| `history` | All | Version history với sizes + contributors |
| `comment` | All | Add file comment (max 10K chars) |
| `comments` | All | List all comments on a file |

### 3.3 Scope model

```
Scope = (team_id, channel, chat_id)

Ví dụ:
  (team-ABC, telegram, -100123456)  ← Telegram group
  (team-ABC, discord, 98765)        ← Discord channel
  (team-ABC, ws, user:123)          ← WS dashboard session
```

- **Channel scope** (mặc định): mỗi channel+chat có workspace riêng
- **Team scope** (`workspace_enabled: true`): shared workspace cross-channel
- Scope resolved từ context (agent loop) hoặc explicit args

### 3.4 Advisory lock & concurrent write safety

```
Agent A writes report.md        Agent B writes report.md
         │                               │
         ▼                               ▼
  pg_try_advisory_xact_lock       pg_try_advisory_xact_lock
  (hashtext("team:ch:chat:report.md"))
         │                               │
      ✓ acquired                      ✗ ErrFileLocked
         │                               │
    disk write + DB upsert          "file being written by
         │                           another agent, try again"
      tx commit → lock released
```

Lock key = `hashtext(team_id || channel || chat_id || file_name)`. Auto-released khi transaction commit.

### 3.5 Versioning

```
report.md (current)
report.md.v1 (previous)
report.md.v2 (2 versions ago)
...
Max 5 versions. Older auto-pruned (disk + DB).
```

Mỗi lần write file đã tồn tại → rename current → `.vN` → write new. `PruneOldVersions` trả về file paths đã xóa để cleanup disk.

### 3.6 Templates

Team settings cho phép define templates auto-seed khi scope được write lần đầu:

```json
{
  "workspace_templates": [
    { "file_name": "README.md", "content": "# Project\n..." },
    { "file_name": "NOTES.md", "content": "# Notes\n..." }
  ]
}
```

Templates được seed cùng lúc với file đầu tiên được write vào scope mới.

### 3.7 Quota & limits

| Limit | Default | Configurable |
|-------|---------|-------------|
| Max file size | 10 MB | Hardcoded |
| Max files per scope | 100 | Hardcoded |
| Max file name length | 100 chars | Hardcoded |
| Workspace quota | Unlimited | `workspace_quota_mb` setting |
| Max comment length | 10,000 chars | Hardcoded |

---

## 4. Delegation Engine

### 4.1 Flow tổng quan

```
Lead agent nhận user request
        │
        ▼
  Quyết định delegate (LLM)
        │
        ▼
  delegate tool → prepareDelegation
        │
        ├─ Auto-create team task (if team_task_id omitted)
        │   └─ task_type = "delegation", auto-claim by target
        │
        ├─ Inject dependency results (blocked_by task results)
        │
        ├─ Inject workspace context (list files in scope)
        │
        └─ Spawn target agent run
              │
              ├─ Target agent works...
              │   ├─ progress updates → renew lock
              │   ├─ workspace writes → deliverables
              │   └─ team_message → communicate
              │
              ▼
        Delegation completes
              │
         ┌────┴────┐
     success     failure
         │           │
         ▼           ▼
  autoCompleteTask  autoFailTask
         │           │
         ├─ update result       ├─ mark failed
         ├─ archive workspace   ├─ record event
         ├─ record event        └─ persist error
         ├─ persist history
         └─ flush delegate session
```

### 4.2 Auto-task creation

Khi lead gọi `delegate` mà không pass `team_task_id`:
- Tự tạo pending task: subject = truncated prompt, task_type = "delegation"
- Auto-claim cho target agent (prevent pending notification spam)
- Điều này đảm bảo mọi delegation đều có task record để track

### 4.3 Dependency injection

Nếu task có `blocked_by` đã completed, delegation context được prepend:

```
[Dependency results from blocking tasks]
Task "Research market data" (completed):
  Result: Market analysis shows...

---
[Your task]
Write report based on the research above.
```

### 4.4 Workspace context injection

Trước khi delegate, inject danh sách workspace files available:

```
[Team workspace files in this scope]
- report-draft.md (12KB, pinned)
- data.csv (45KB)
Use workspace_read to access file contents.
```

### 4.5 Progress notifications

Lead agent nhận progress update grouped per tick:

```
[Delegation progress]
• tieu-ho-shopee-1: 💭 thinking (2m 30s)
• researcher-bot: 🔍 web_search (1m 15s)
```

Controlled by team setting `progress_notifications` (default: on).

---

## 5. Team Messaging

### 5.1 Actions

| Action | Quyền | Mô tả |
|--------|-------|-------|
| `send` | All | Direct message to teammate, auto-creates message task |
| `broadcast` | Lead | Message all members |
| `read` | All | Fetch unread, auto-mark read |

### 5.2 Message task auto-creation

Khi agent gửi `send`, system tạo task:
- `task_type = "message"`, `status = in_progress`
- `owner_agent_id = recipient` (locked 30 min)
- Subject = truncated message (100 chars)
- Purpose: đảm bảo message không bị lost, recipient có task context

---

## 6. Team Settings

Stored as JSONB in `agent_teams.settings`:

```json
{
  // Access control
  "allow_user_ids": ["user:telegram:123"],
  "deny_user_ids": [],
  "allow_channels": ["telegram"],
  "deny_channels": [],

  // Workspace
  "workspace_enabled": true,
  "workspace_templates": [
    { "file_name": "README.md", "content": "..." }
  ],
  "workspace_quota_mb": 100,

  // Follow-up reminders
  "followup_interval_minutes": 30,    // default: 30
  "followup_max_reminders": 5,        // default: 0 (unlimited)

  // Delegation
  "progress_notifications": true,

  // Escalation policy
  "escalation_mode": "auto",          // auto | review | reject
  "escalation_actions": ["pin", "unpin", "tag", "set_template", "delete_others"]
}
```

### Access control

- `allow_user_ids` + `deny_user_ids`: whitelist/blacklist users
- `allow_channels` + `deny_channels`: whitelist/blacklist channels
- System channels (delegate, system) luôn bypass

### Escalation policy

Lead-only actions nhạy cảm (pin, tag, delete file người khác) qua escalation:

| Mode | Behavior |
|------|----------|
| `auto` (default) | LLM tự quyết reject hoặc tạo review task. Không bao giờ tự execute |
| `review` | Luôn tạo pending task cho human approve |
| `reject` | Từ chối thẳng, không execute |

---

## 7. Kết nối giữa các subsystem

### Tasks ↔ Workspace

- **Attachments**: task có thể link workspace files qua `team_task_attachments`
- **Archive on complete**: khi task completed, workspace files linked bị soft-delete (trừ pinned)
- **Deliverables**: delegation result có thể include file paths từ workspace
- **Tag "deliverable"**: mark file là output chính của task

### Tasks ↔ Delegation

- Mỗi delegation tạo 1 task record (auto hoặc explicit)
- Task auto-complete/fail khi delegation kết thúc
- Dependency injection: task blocked_by results inject vào delegation context
- Delegation history persisted riêng cho analytics

### Tasks ↔ Messaging

- `team_message send` tạo message task cho recipient
- Team messages stored trong `team_messages` table (mailbox pattern)
- Agent `read` unread messages khi cần

### Workspace ↔ Delegation

- Workspace context inject vào delegate trước khi chạy
- Delegate agent có thể read/write workspace files trong scope
- Scope dùng origin channel (không phải "delegate" channel)

---

## 8. RBAC

Roles per team member:

| Role | Tasks | Workspace | Messaging |
|------|-------|-----------|-----------|
| **Lead** | Full: create, cancel, update, assign | Full: pin, tag, set_template, delete any | Broadcast + send |
| **Member** | Claim, complete, own tasks | Write, delete own, comment | Send + read |
| **Reviewer** | Read-only, comment | Read-only, comment | Read |

---

## 9. Database schema (migrations)

| Migration | Nội dung |
|-----------|----------|
| 003 | `team_tasks` base table |
| 004 | FTS: `tsv` tsvector column |
| 007 | `metadata` JSONB |
| 008 | `user_id`, `channel` columns |
| 018 | V2: task_type, task_number, identifier, created_by, assignee, parent_id, chat_id, locking, progress + comments/events/attachments tables |
| 019 | Follow-up: followup_at/count/max/message/channel/chat_id |

---

## 10. System prompt injection

Khi agent trong team, system prompt được inject:

- **TEAM.md**: team name, members list (key + display_name + role), lead agent, access scope
- **DELEGATION.md**: available target agents, links, max_concurrent, max_delegation_load
- **AVAILABILITY.md**: workspace status, pending tasks count
- Skip on bootstrap (first run) để giảm noise

---

## 11. Future ideas

### Team Playbook (planned)

File `TEAM_PLAYBOOK.md` per team — human-authored strategy cho lead:
- Member capabilities: ai giỏi gì
- Delegation strategy: task nào assign cho ai
- Rules: max concurrent tasks, escalation policy

### Member Capabilities (planned)

```json
// agent_team_members.capabilities JSONB
{
  "skills": ["coding", "research"],
  "languages": ["go", "python"],
  "max_concurrent_tasks": 2
}
```

### Auto-learned patterns (future)

Track delegation success rate per member per task type → suggest optimal assignment.

---

## 12. Known design decisions

1. **Followup on pending = vô nghĩa** — stale recovery clear followup fields khi reset task về pending
2. **Process followups TRƯỚC recovery** — đảm bảo followup fire trước khi task bị reset status
3. **Advisory lock per file** — `pg_try_advisory_xact_lock(hashtext(...))`, fail-fast thay vì wait
4. **Task identifier per team** — sequential number + team prefix, không dùng global sequence
5. **30 min lock** — progress update renew, balance giữa agent timeout và stale recovery
6. **Auto followup là fallback** — LLM vẫn có thể gọi `await_reply` để customize, system chỉ fill default khi LLM không gọi
