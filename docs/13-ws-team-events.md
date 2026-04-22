# WebSocket Team & Delegation Events

Complete reference for all WS events related to team operations, delegation lifecycle, and admin CRUD.

---

## 1. Overview

Events are emitted via `msgBus.Broadcast(bus.Event{})` and forwarded to all connected WS clients by the gateway subscriber. The domain event bus (`internal/eventbus`) is internal-only and does **not** forward to WS clients.

Wire frame:
```json
{"type": "event", "event": "<event_name>", "payload": { ... }}
```

---

## 2. Event Envelope

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"event"` |
| `event` | string | Event name (e.g. `"delegation.started"`) |
| `payload` | object | Event-specific payload (see §3) |

---

## 3. Payload Base Types

Defined in `pkg/protocol/team_events.go`. Each event in §4 lists which base it uses plus any delta fields.

### 3.1 DelegationEventPayload

Used by: `delegation.started/completed/failed/cancelled`.

Fields: `delegation_id`, `source_agent_id`, `source_agent_key`, `source_display_name?`, `target_agent_id`, `target_agent_key`, `target_display_name?`, `user_id`, `channel`, `chat_id`, `mode` (`async`/`sync`), `task?`, `team_id?`, `team_task_id?`, `status?`, `elapsed_ms?`, `error?`, `created_at`.

```json
{
  "delegation_id": "a1b2c3d4",
  "source_agent_id": "<source-agent-id>",
  "source_agent_key": "default",
  "source_display_name": "Default Agent",
  "target_agent_id": "<target-agent-id>",
  "target_agent_key": "content-writer",
  "target_display_name": "Content Writer",
  "user_id": "<user-id>",
  "channel": "telegram",
  "chat_id": "<chat-id>",
  "mode": "async",
  "task": "Create Instagram image",
  "team_id": "<team-id>",
  "team_task_id": "<team-task-id>",
  "status": "running",
  "created_at": "2026-03-05T10:00:00Z"
}
```

### 3.2 DelegationProgressPayload

Used by: `delegation.progress`.

Fields: `source_agent_id`, `source_agent_key`, `user_id`, `channel`, `chat_id`, `team_id?`, `active_delegations[]` — each item: `delegation_id`, `target_agent_key`, `target_display_name?`, `elapsed_ms`, `team_task_id?`, `activity?` (`thinking`/`tool_exec`/`compacting`), `tool?`.

```json
{
  "source_agent_id": "<source-agent-id>",
  "source_agent_key": "default",
  "user_id": "<user-id>",
  "channel": "telegram",
  "chat_id": "<chat-id>",
  "team_id": "<team-id>",
  "active_delegations": [
    {"delegation_id": "a1b2c3d4", "target_agent_key": "content-writer", "elapsed_ms": 45000, "activity": "tool_exec", "tool": "create_image"}
  ]
}
```

### 3.3 DelegationAccumulatedPayload

Used by: `delegation.accumulated`.

Fields: `delegation_id`, `source_agent_id`, `source_agent_key`, `target_agent_key`, `target_display_name?`, `user_id`, `channel`, `chat_id`, `team_id?`, `team_task_id?`, `siblings_remaining`, `elapsed_ms?`.

```json
{
  "delegation_id": "a1b2c3d4",
  "source_agent_id": "<source-agent-id>",
  "source_agent_key": "default",
  "target_agent_key": "content-writer",
  "user_id": "<user-id>",
  "channel": "telegram",
  "chat_id": "<chat-id>",
  "team_id": "<team-id>",
  "team_task_id": "<team-task-id>",
  "siblings_remaining": 1,
  "elapsed_ms": 45300
}
```

### 3.4 DelegationAnnouncePayload

Used by: `delegation.announce`.

Fields: `source_agent_id`, `source_agent_key`, `source_display_name?`, `user_id`, `channel`, `chat_id`, `team_id?`, `results[]` (`agent_key`, `display_name?`, `has_media`, `content_preview?`), `completed_task_ids?[]`, `total_elapsed_ms`, `has_media`.

```json
{
  "source_agent_id": "<source-agent-id>",
  "source_agent_key": "default",
  "source_display_name": "Default Agent",
  "user_id": "<user-id>",
  "channel": "telegram",
  "chat_id": "<chat-id>",
  "team_id": "<team-id>",
  "results": [
    {"agent_key": "content-writer", "display_name": "Content Writer", "has_media": true, "content_preview": "Created image..."},
    {"agent_key": "copywriter", "has_media": false, "content_preview": "Wrote caption..."}
  ],
  "completed_task_ids": ["<task-id-1>", "<task-id-2>"],
  "total_elapsed_ms": 52000,
  "has_media": true
}
```

### 3.5 TeamTaskEventPayload

Used by: all `team.task.*` events.

Fields: `team_id`, `task_id`, `task_number?`, `subject?`, `status`, `owner_agent_key?`, `owner_display_name?`, `reason?`, `user_id`, `channel`, `chat_id`, `peer_kind?` (`group`/`direct`), `local_key?`, `timestamp`, `comment_text?`, `progress_percent?`, `progress_step?`, `actor_type?` (`agent`/`human`/`system`), `actor_id?`.

```json
{
  "team_id": "<team-id>",
  "task_id": "<task-id>",
  "task_number": 5,
  "subject": "Create Instagram image",
  "status": "pending",
  "owner_agent_key": "",
  "user_id": "<user-id>",
  "channel": "dashboard",
  "chat_id": "<chat-id>",
  "timestamp": "2026-03-05T10:00:00Z",
  "actor_type": "human",
  "actor_id": "<user-id>"
}
```

### 3.6 Team CRUD / Misc Payloads

**TeamCreatedPayload** (`team.created`): `team_id`, `team_name`, `lead_agent_key`, `lead_display_name?`, `member_count`.

**TeamUpdatedPayload** (`team.updated`): `team_id`, `team_name`, `changes[]`.

**TeamDeletedPayload** (`team.deleted`): `team_id`, `team_name`.

**TeamMemberAddedPayload** (`team.member.added`): `team_id`, `team_name`, `agent_id`, `agent_key`, `display_name?`, `role`.

**TeamMemberRemovedPayload** (`team.member.removed`): `team_id`, `team_name`, `agent_id`, `agent_key`, `display_name?`.

**TeamMessageEventPayload** (`team.message.sent`): `team_id`, `from_agent_key`, `from_display_name?`, `to_agent_key` (`"broadcast"` for team broadcast), `to_display_name?`, `message_type`, `preview`, `task_id?`, `user_id`, `channel`, `chat_id`.

**AgentLinkCreatedPayload** (`agent_link.created`): `link_id`, `source_agent_id`, `source_agent_key`, `target_agent_id`, `target_agent_key`, `direction`, `team_id?`, `status`.

**AgentLinkUpdatedPayload** (`agent_link.updated`): `link_id`, `source_agent_key`, `target_agent_key`, `direction?`, `status?`, `changes[]`.

**AgentLinkDeletedPayload** (`agent_link.deleted`): `link_id`, `source_agent_key`, `target_agent_key`.

---

## 4. Event Catalog

### 4.1 Delegation Events

| Event | Base | Delta Fields | Notes |
|-------|------|-------------|-------|
| `delegation.started` | `DelegationEventPayload` | `status="running"` | Lead initiates delegation to member |
| `delegation.completed` | `DelegationEventPayload` | `status="completed"`, `+elapsed_ms` | Member completed successfully |
| `delegation.failed` | `DelegationEventPayload` | `status="failed"`, `+elapsed_ms`, `+error` | Member failed |
| `delegation.cancelled` | `DelegationEventPayload` | `status="cancelled"`, `+elapsed_ms` | Via `/stopall`, task cancel, or direct cancel |
| `delegation.progress` | `DelegationProgressPayload` | — | Periodic (~30s) snapshot of all active async delegations |
| `delegation.accumulated` | `DelegationAccumulatedPayload` | — | Member done but siblings still running; result held |
| `delegation.announce` | `DelegationAnnouncePayload` | — | Last sibling done; all results returned to lead |

### 4.2 Team Task Events

| Event | Base | Delta Fields | Notes |
|-------|------|-------------|-------|
| `team.task.created` | `TeamTaskEventPayload` | `status="pending"` | New task (manual or via delegation) |
| `team.task.assigned` | `TeamTaskEventPayload` | `status="in_progress"`, `+owner_agent_key` | Auto-assigned at creation or via `teams.tasks.assign` RPC |
| `team.task.dispatched` | `TeamTaskEventPayload` | `status="in_progress"`, `+owner_agent_key` | Leader dispatches pending task to member |
| `team.task.claimed` | `TeamTaskEventPayload` | `status="in_progress"`, `+owner_agent_key`, `+owner_display_name` | Reserved; not currently emitted |
| `team.task.progress` | `TeamTaskEventPayload` | `+progress_percent`, `+progress_step` | Member calls `team_tasks(action="progress")` |
| `team.task.reviewed` | `TeamTaskEventPayload` | `status="in_review"`, `+owner_agent_key`, `+owner_display_name` | Member submits for review |
| `team.task.approved` | `TeamTaskEventPayload` | `status="completed"` | Human approves via dashboard |
| `team.task.rejected` | `TeamTaskEventPayload` | `status="cancelled"`, `+reason` | Human rejects via dashboard |
| `team.task.completed` | `TeamTaskEventPayload` | `status="completed"`, `+owner_agent_key`, `+owner_display_name` | Auto-completed by delegation or agent |
| `team.task.failed` | `TeamTaskEventPayload` | `status="failed"`, `+reason`, `+owner_agent_key`, `+task_number`, `+subject` | Blocker escalation or system error |
| `team.task.cancelled` | `TeamTaskEventPayload` | `status="cancelled"`, `+reason?` | Explicit cancel |
| `team.task.stale` | `TeamTaskEventPayload` | `status="stale"`, `+reason` | No activity within timeout |
| `team.task.commented` | `TeamTaskEventPayload` | `+comment_text` | Blocker-type comment also auto-fails task + escalates lead |
| `team.task.updated` | `TeamTaskEventPayload` | `+changes[]` | Metadata update via `team_tasks(action="update")` |
| `team.task.deleted` | `TeamTaskEventPayload` | `status=<terminal>` | Hard-delete of terminal-status task |
| `team.task.attachment_added` | `TeamTaskEventPayload` | `+task_number`, `+subject` | File attached via `team_tasks(action="attach")` |

### 4.3 Team Leader Events

| Event | Payload | Notes |
|-------|---------|-------|
| `team.leader.processing` | `{agentId: string, tasks: int}` | Bridge: announce queue draining; fires before leader `run.started`. Emitted from `cmd/gateway_subagent_announce_queue.go` |

### 4.4 Team CRUD Events (Admin)

No routing context (`user_id`/`channel`/`chat_id`) — admin operations only.

| Event | Base |
|-------|------|
| `team.created` | `TeamCreatedPayload` |
| `team.updated` | `TeamUpdatedPayload` |
| `team.deleted` | `TeamDeletedPayload` |
| `team.member.added` | `TeamMemberAddedPayload` |
| `team.member.removed` | `TeamMemberRemovedPayload` |

### 4.5 Agent Link Events (Admin)

| Event | Base |
|-------|------|
| `agent_link.created` | `AgentLinkCreatedPayload` |
| `agent_link.updated` | `AgentLinkUpdatedPayload` |
| `agent_link.deleted` | `AgentLinkDeletedPayload` |

### 4.6 Workspace Events

| Event | Payload | Notes |
|-------|---------|-------|
| `workspace.file.changed` | `{team_id, chat_id, file_name, change_type, timestamp}` | Reserved — not currently emitted. `change_type`: `created`/`modified`/`deleted` |

### 4.7 Team Message Events

| Event | Base | Notes |
|-------|------|-------|
| `team.message.sent` | `TeamMessageEventPayload` | `to_agent_key="broadcast"` for team-wide messages |

### 4.8 Agent Events (Delegation Context)

Agent events use top-level `"event": "agent"`. The `type` field inside the payload is the subtype. When running inside a delegation, extra context fields are present.

Delegation-only fields: `delegationId`, `teamId`, `teamTaskId`, `parentAgentId` (lead agent key). Always-present when available: `userId`, `channel`, `chatId`. Distinguish lead vs member: `parentAgentId` absent = lead, present = member.

| Constant | Type | Description |
|----------|------|-------------|
| `AgentEventRunStarted` | `run.started` | Agent run begins |
| `AgentEventRunCompleted` | `run.completed` | Agent run finished |
| `AgentEventRunFailed` | `run.failed` | Agent run failed |
| `AgentEventRunCancelled` | `run.cancelled` | Agent run cancelled |
| `AgentEventRunRetrying` | `run.retrying` | Retrying after error |
| `AgentEventToolCall` | `tool.call` | Tool invoked — name + call ID only (no args) |
| `AgentEventToolResult` | `tool.result` | Tool done — name + call ID + `is_error` (no content) |
| `AgentEventBlockReply` | `block.reply` | Block-level reply |
| `AgentEventActivity` | `activity` | Phase: `thinking`, `tool_exec`, `compacting` |
| *(chat)* | `chunk` | Streaming text fragment |
| *(chat)* | `thinking` | Extended thinking content |
| *(chat)* | `message` | Full message (non-streaming) |

> When `Stream: true`, `chunk`/`thinking` emit incrementally. When `Stream: false` (delegate runs), emit once with full content.

---

## 5. Correlation Rules

| Source Event | Correlated Event(s) | Key | Notes |
|-------------|---------------------|-----|-------|
| `delegation.started` | `delegation.completed/failed/cancelled` | `delegation_id` | Lifecycle pair |
| `delegation.completed` (async) | `delegation.accumulated` → `delegation.announce` | `delegation_id`, `team_id` | Accumulated per member; announce once when last sibling done |
| `delegation.progress` | — | `source_agent_id` | Groups all active delegations from same leader |
| `team.task.created` | `team.task.assigned` / `team.task.dispatched` | `task_id` | Assignment follows creation |
| `team.task.reviewed` | `team.task.approved` / `team.task.rejected` | `task_id` | Human review loop |
| `team.task.commented` (blocker) | `team.task.failed` | `task_id` | Blocker comment auto-fails task + escalates lead |
| last `team.task.completed` (batch) | `team.leader.processing` → leader `run.started` | `team_id`, `agentId` | Bridge: announce queue start → leader run |
| `agent run.started` (member) | `agent run.completed/failed` | `runId`, `delegationId` | Member run lifecycle |

---

## 6. Constants Reference

All event name constants are defined in `pkg/protocol/events.go`:

### Delegation Lifecycle Events
| Constant | Event Name |
|----------|-----------|
| `EventDelegationStarted` | `delegation.started` |
| `EventDelegationCompleted` | `delegation.completed` |
| `EventDelegationFailed` | `delegation.failed` |
| `EventDelegationCancelled` | `delegation.cancelled` |
| `EventDelegationProgress` | `delegation.progress` |
| `EventDelegationAccumulated` | `delegation.accumulated` |
| `EventDelegationAnnounce` | `delegation.announce` |

### Team Task Lifecycle Events
| Constant | Event Name | Status |
|----------|-----------|--------|
| `EventTeamTaskCreated` | `team.task.created` | Active |
| `EventTeamTaskClaimed` | `team.task.claimed` | Reserved (not emitted) |
| `EventTeamTaskAssigned` | `team.task.assigned` | Active |
| `EventTeamTaskDispatched` | `team.task.dispatched` | Active |
| `EventTeamTaskCompleted` | `team.task.completed` | Active |
| `EventTeamTaskCancelled` | `team.task.cancelled` | Active |
| `EventTeamTaskApproved` | `team.task.approved` | Active |
| `EventTeamTaskRejected` | `team.task.rejected` | Active |
| `EventTeamTaskCommented` | `team.task.commented` | Active |
| `EventTeamTaskDeleted` | `team.task.deleted` | Active |
| `EventTeamTaskFailed` | `team.task.failed` | Active |
| `EventTeamTaskReviewed` | `team.task.reviewed` | Active |
| `EventTeamTaskProgress` | `team.task.progress` | Active |
| `EventTeamTaskUpdated` | `team.task.updated` | Active |
| `EventTeamTaskStale` | `team.task.stale` | Active |
| `EventTeamTaskAttachmentAdded` | `team.task.attachment_added` | Active |
| `EventTeamLeaderProcessing` | `team.leader.processing` | Active |

### Team CRUD Events
| Constant | Event Name |
|----------|-----------|
| `EventTeamCreated` | `team.created` |
| `EventTeamUpdated` | `team.updated` |
| `EventTeamDeleted` | `team.deleted` |
| `EventTeamMemberAdded` | `team.member.added` |
| `EventTeamMemberRemoved` | `team.member.removed` |

### Agent Link Events
| Constant | Event Name |
|----------|-----------|
| `EventAgentLinkCreated` | `agent_link.created` |
| `EventAgentLinkUpdated` | `agent_link.updated` |
| `EventAgentLinkDeleted` | `agent_link.deleted` |

### Workspace Events
| Constant | Event Name | Status |
|----------|-----------|--------|
| `EventWorkspaceFileChanged` | `workspace.file.changed` | Reserved (future) |

### Team Message Events
| Constant | Event Name |
|----------|-----------|
| `EventTeamMessageSent` | `team.message.sent` |

**Payload structs** in `pkg/protocol/team_events.go`: `DelegationEventPayload`, `DelegationProgressPayload`, `DelegationAccumulatedPayload`, `DelegationAnnouncePayload`, `TeamTaskEventPayload`, `TeamMessageEventPayload`, `TeamCreatedPayload`, `TeamUpdatedPayload`, `TeamDeletedPayload`, `TeamMemberAddedPayload`, `TeamMemberRemovedPayload`, `AgentLinkCreatedPayload`, `AgentLinkUpdatedPayload`, `AgentLinkDeletedPayload`.
