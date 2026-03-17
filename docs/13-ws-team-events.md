# WebSocket Team & Delegation Events

Complete reference for all WS events related to team agent operations, delegation lifecycle, and admin CRUD.

All events are delivered as JSON frames via the WebSocket protocol:
```json
{"type": "event", "event": "<event_name>", "payload": { ... }}
```

Events are emitted via `msgBus.Broadcast(bus.Event{})` and forwarded to all connected WS clients (filtered by gateway subscriber at `server.go`).

---

## Event Catalog

### Delegation Lifecycle Events

#### `delegation.started`
Emitted when a lead agent initiates a delegation to a member agent.

```json
{
  "delegation_id": "a1b2c3d4",
  "source_agent_id": "019c839b-...",
  "source_agent_key": "default",
  "source_display_name": "Default Agent",
  "target_agent_id": "019ca748-...",
  "target_agent_key": "tieu-la",
  "target_display_name": "Tieu La",
  "user_id": "user123",
  "channel": "telegram",
  "chat_id": "-100123456",
  "mode": "async",
  "task": "Create Instagram image for new product",
  "team_id": "019c9503-...",
  "team_task_id": "019ca84f-...",
  "status": "running",
  "created_at": "2026-03-05T10:00:00Z"
}
```

#### `delegation.completed`
Emitted when a delegation finishes successfully (quality gates passed).

Same payload as `delegation.started` with:
- `status`: `"completed"`
- `elapsed_ms`: total duration in milliseconds

#### `delegation.failed`
Emitted when a delegation fails (agent error or quality gate rejection).

Same payload as `delegation.started` with:
- `status`: `"failed"`
- `error`: error message string
- `elapsed_ms`: total duration

#### `delegation.cancelled`
Emitted when a delegation is cancelled (via `/stopall`, team task cancel, or direct cancel).

Same payload as `delegation.started` with:
- `status`: `"cancelled"`
- `elapsed_ms`: total duration

#### `delegation.progress`
Emitted periodically (~30s) for active async delegations. Groups all active delegations from the same source agent.

```json
{
  "source_agent_id": "019c839b-...",
  "source_agent_key": "default",
  "user_id": "user123",
  "channel": "telegram",
  "chat_id": "-100123456",
  "team_id": "019c9503-...",
  "active_delegations": [
    {
      "delegation_id": "a1b2c3d4",
      "target_agent_key": "tieu-la",
      "target_display_name": "Tieu La",
      "elapsed_ms": 45000,
      "team_task_id": "019ca84f-..."
    },
    {
      "delegation_id": "e5f6g7h8",
      "target_agent_key": "tieu-ngon",
      "target_display_name": "Tieu Ngon",
      "elapsed_ms": 30000,
      "team_task_id": "019ca850-..."
    }
  ]
}
```

#### `delegation.accumulated`
Emitted when an async delegation completes but sibling delegations are still running. The result is accumulated and will be announced when all siblings finish.

```json
{
  "delegation_id": "a1b2c3d4",
  "source_agent_id": "019c839b-...",
  "source_agent_key": "default",
  "target_agent_key": "tieu-la",
  "target_display_name": "Tieu La",
  "user_id": "user123",
  "channel": "telegram",
  "chat_id": "-100123456",
  "team_id": "019c9503-...",
  "team_task_id": "019ca84f-...",
  "siblings_remaining": 1,
  "elapsed_ms": 45300
}
```

#### `delegation.announce`
Emitted when the last sibling delegation completes and all accumulated results are sent back to the lead agent.

```json
{
  "source_agent_id": "019c839b-...",
  "source_agent_key": "default",
  "source_display_name": "Default Agent",
  "user_id": "user123",
  "channel": "telegram",
  "chat_id": "-100123456",
  "team_id": "019c9503-...",
  "results": [
    {
      "agent_key": "tieu-la",
      "display_name": "Tieu La",
      "has_media": true,
      "content_preview": "Created Instagram post image..."
    },
    {
      "agent_key": "tieu-ngon",
      "display_name": "Tieu Ngon",
      "has_media": false,
      "content_preview": "Wrote caption for you..."
    }
  ],
  "completed_task_ids": ["019ca84f-...", "019ca850-..."],
  "total_elapsed_ms": 52000,
  "has_media": true
}
```

#### `delegation.quality_gate.retry`
Emitted when a quality gate rejects a delegation result and triggers a retry.

```json
{
  "delegation_id": "a1b2c3d4",
  "target_agent_key": "tieu-la",
  "user_id": "user123",
  "channel": "telegram",
  "chat_id": "-100123456",
  "team_id": "019c9503-...",
  "team_task_id": "019ca84f-...",
  "gate_type": "agent",
  "attempt": 2,
  "max_retries": 3,
  "feedback": "Image aspect ratio should be 4:5..."
}
```

---

### Team Task Events

#### `team.task.created`
Emitted when a new team task is created (manual or auto-created by delegation).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "subject": "Create Instagram image",
  "status": "pending",
  "owner_agent_key": "",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:00:00Z",
  "actor_type": "human",
  "actor_id": "user123"
}
```

#### `team.task.claimed`
Emitted when an agent claims a task (reserved for future use; not currently emitted).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "in_progress",
  "owner_agent_key": "tieu-la",
  "owner_display_name": "Tieu La",
  "user_id": "user123",
  "channel": "system",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:00:01Z"
}
```

#### `team.task.assigned`
Emitted when a task is assigned to an agent (either auto-assigned at creation or manually via `teams.tasks.assign` RPC).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "in_progress",
  "owner_agent_key": "tieu-la",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:00:01Z",
  "actor_type": "human",
  "actor_id": "user123"
}
```

#### `team.task.completed`
Emitted when a task is completed (auto-completed by delegation or marked complete by agent).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "completed",
  "owner_agent_key": "tieu-la",
  "owner_display_name": "Tieu La",
  "user_id": "user123",
  "channel": "system",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:00:45Z"
}
```

#### `team.task.cancelled`
Emitted when a task is cancelled. Separated from `team.task.completed` for correct semantics.

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "cancelled",
  "reason": "Task no longer needed",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:01:00Z"
}
```

#### `team.task.approved`
Emitted when a human approves a task via the dashboard (status becomes completed).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "completed",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:02:00Z",
  "actor_type": "human",
  "actor_id": "user123"
}
```

#### `team.task.rejected`
Emitted when a human rejects a task via the dashboard (status becomes cancelled with reason).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "cancelled",
  "reason": "Image aspect ratio should be 4:5",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:02:30Z",
  "actor_type": "human",
  "actor_id": "user123"
}
```

#### `team.task.commented`
Emitted when a human adds a comment to a task (no status change).

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:02:45Z"
}
```

#### `team.task.deleted`
Emitted when a terminal-status task is hard-deleted via the dashboard.

```json
{
  "team_id": "019c9503-...",
  "task_id": "019ca84f-...",
  "status": "completed",
  "user_id": "user123",
  "channel": "dashboard",
  "chat_id": "-100123456",
  "timestamp": "2026-03-05T10:03:00Z",
  "actor_type": "human",
  "actor_id": "user123"
}
```

#### `team.task.failed`
Reserved for future use. Emitted when a task auto-execution fails (not currently triggered).

#### `team.task.reviewed`
Reserved for future use. Emitted when a task enters review stage (not currently triggered).

#### `team.task.progress`
Reserved for future use. Emitted periodically for long-running tasks (not currently triggered).

#### `team.task.updated`
Reserved for future use. Emitted when task metadata is updated (not currently triggered).

#### `team.task.stale`
Reserved for future use. Emitted when a task hasn't been updated within a timeout threshold (not currently triggered).

---

### Workspace Events

#### `workspace.file.changed`
Emitted when a file in a team's workspace directory is created, modified, or deleted. This event is reserved for future implementation; not currently broadcasted.

```json
{
  "team_id": "019c9503-...",
  "chat_id": "-100123456",
  "file_name": "project/notes.md",
  "change_type": "created",
  "timestamp": "2026-03-05T10:00:00Z"
}
```

**Potential field values:**
- `change_type`: `"created"`, `"modified"`, `"deleted"`

---

### Team Message Events

#### `team.message.sent`
Emitted when an agent sends a message to another agent or broadcasts to the team.

```json
{
  "team_id": "019c9503-...",
  "from_agent_key": "default",
  "from_display_name": "Default Agent",
  "to_agent_key": "tieu-la",
  "to_display_name": "Tieu La",
  "message_type": "chat",
  "preview": "Please create an Instagram image...",
  "task_id": "",
  "user_id": "user123",
  "channel": "telegram",
  "chat_id": "-100123456"
}
```

For broadcast messages: `to_agent_key = "broadcast"`, `to_display_name = ""`.

---

### Team CRUD Events (Admin)

These events are emitted from RPC handlers when teams are managed via the Web UI. No routing context (user_id/channel/chat_id) since these are admin operations.

#### `team.created`
```json
{
  "team_id": "019c9503-...",
  "team_name": "Content Team",
  "lead_agent_key": "default",
  "lead_display_name": "Default Agent",
  "member_count": 3
}
```

#### `team.updated`
```json
{
  "team_id": "019c9503-...",
  "team_name": "Content Team",
  "changes": ["settings"]
}
```

#### `team.deleted`
```json
{
  "team_id": "019c9503-...",
  "team_name": "Content Team"
}
```

#### `team.member.added`
```json
{
  "team_id": "019c9503-...",
  "team_name": "Content Team",
  "agent_id": "019ca748-...",
  "agent_key": "tieu-la",
  "display_name": "Tieu La",
  "role": "member"
}
```

#### `team.member.removed`
```json
{
  "team_id": "019c9503-...",
  "team_name": "Content Team",
  "agent_id": "019ca748-...",
  "agent_key": "tieu-la",
  "display_name": "Tieu La"
}
```

---

### Agent Events (Delegation Context)

`AgentEvent` payloads (broadcast as `"event": "agent"`) now include optional delegation and routing context:

**`tool.call` example** (member agent inside delegation):
```json
{
  "type": "tool.call",
  "agentId": "tieu-la",
  "runId": "delegate-a1b2c3d4",
  "delegationId": "a1b2c3d4",
  "teamId": "019c9503-...",
  "teamTaskId": "019ca84f-...",
  "parentAgentId": "default",
  "userId": "user123",
  "channel": "telegram",
  "chatId": "-100123456",
  "payload": {"name": "create_image", "id": "call_xxx"}
}
```

**`tool.result` example:**
```json
{
  "type": "tool.result",
  "agentId": "tieu-la",
  "runId": "delegate-a1b2c3d4",
  "delegationId": "a1b2c3d4",
  "teamId": "019c9503-...",
  "teamTaskId": "019ca84f-...",
  "parentAgentId": "default",
  "userId": "user123",
  "channel": "telegram",
  "chatId": "-100123456",
  "payload": {"name": "create_image", "id": "call_xxx", "is_error": false}
}
```

> **Note:** Tool arguments and result content are intentionally omitted from payloads to avoid leaking sensitive data over WS. Only tool name, call ID, and error status are included.

**Agent event subtypes** (the `type` field inside the payload):

| Constant | Type | Description |
|----------|------|-------------|
| `AgentEventRunStarted` | `run.started` | Agent run begins |
| `AgentEventRunCompleted` | `run.completed` | Agent run finished successfully |
| `AgentEventRunFailed` | `run.failed` | Agent run failed |
| `AgentEventRunRetrying` | `run.retrying` | Agent run retrying after error |
| `AgentEventToolCall` | `tool.call` | Agent calling a tool |
| `AgentEventToolResult` | `tool.result` | Tool execution completed |
| *(chat events)* | `chunk` | Streaming text chunk |
| *(chat events)* | `thinking` | Extended thinking content |

**Note:** When `Stream: true`, `chunk` and `thinking` are emitted incrementally (one event per streamed fragment). When `Stream: false` (e.g. delegate runs), they are emitted as a single event containing the full content after the LLM response is received. Both paths carry full delegation context.

Fields present only when the agent is running inside a delegation:
- `delegationId` — correlation ID for this delegation
- `teamId` — team scope (if team-based)
- `teamTaskId` — associated team task
- `parentAgentId` — lead agent key that initiated the delegation

Fields always present when available:
- `userId` — scoped user ID (group chats: `"group:{channel}:{chatID}"`)
- `channel` — origin channel (telegram, discord, web, etc.)
- `chatId` — origin chat/conversation ID

Client can distinguish lead vs member agent events:
- `parentAgentId` absent → lead agent event
- `parentAgentId` present → member agent event (delegation)

---

## Event Flow Timeline

```
User sends "Create Instagram post" to Default Agent (lead)
  |
  v
[agent] run.started          agentId=default
[agent] chunk                agentId=default, content="Let me assign..."
[agent] tool.call            agentId=default, tool=delegate
  |
  |-- [delegation.started]   target=tieu-la, task="Create image", mode=async
  |-- [delegation.started]   target=tieu-ngon, task="Write caption", mode=async
  |
  |   (member agents run in parallel)
  |
  |-- [agent] run.started    agentId=tieu-la, delegationId=xxx, parentAgentId=default
  |-- [agent] run.started    agentId=tieu-ngon, delegationId=yyy, parentAgentId=default
  |
  |-- [agent] tool.call      agentId=tieu-la, tool=create_image, delegationId=xxx
  |-- [agent] tool.result    agentId=tieu-la, delegationId=xxx
  |
  |-- [delegation.progress]  active=[{tieu-la, 30s}, {tieu-ngon, 30s}]
  |
  |-- [agent] run.completed  agentId=tieu-la, delegationId=xxx
  |-- [delegation.completed] target=tieu-la, elapsed_ms=35000
  |-- [delegation.accumulated] target=tieu-la, siblings_remaining=1
  |
  |-- [agent] run.completed  agentId=tieu-ngon, delegationId=yyy
  |-- [delegation.completed] target=tieu-ngon, elapsed_ms=42000
  |-- [delegation.announce]  results=[{tieu-la, has_media}, {tieu-ngon}]
  |
  |   (lead receives announce, processes results)
  |
  |-- [agent] run.started    agentId=default
  |-- [agent] chunk          agentId=default, content="Here are the results..."
  +-- [agent] run.completed  agentId=default
```

---

## Constants Reference

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
| `EventQualityGateRetry` | `delegation.quality_gate.retry` |

### Team Task Lifecycle Events
| Constant | Event Name | Status |
|----------|-----------|--------|
| `EventTeamTaskCreated` | `team.task.created` | Active |
| `EventTeamTaskClaimed` | `team.task.claimed` | Reserved (not emitted) |
| `EventTeamTaskAssigned` | `team.task.assigned` | Active |
| `EventTeamTaskCompleted` | `team.task.completed` | Active |
| `EventTeamTaskCancelled` | `team.task.cancelled` | Active |
| `EventTeamTaskApproved` | `team.task.approved` | Active |
| `EventTeamTaskRejected` | `team.task.rejected` | Active |
| `EventTeamTaskCommented` | `team.task.commented` | Active |
| `EventTeamTaskDeleted` | `team.task.deleted` | Active |
| `EventTeamTaskFailed` | `team.task.failed` | Reserved (future) |
| `EventTeamTaskReviewed` | `team.task.reviewed` | Reserved (future) |
| `EventTeamTaskProgress` | `team.task.progress` | Reserved (future) |
| `EventTeamTaskUpdated` | `team.task.updated` | Reserved (future) |
| `EventTeamTaskStale` | `team.task.stale` | Reserved (future) |

### Team CRUD Events
| Constant | Event Name |
|----------|-----------|
| `EventTeamCreated` | `team.created` |
| `EventTeamUpdated` | `team.updated` |
| `EventTeamDeleted` | `team.deleted` |
| `EventTeamMemberAdded` | `team.member.added` |
| `EventTeamMemberRemoved` | `team.member.removed` |

### Workspace Events
| Constant | Event Name | Status |
|----------|-----------|--------|
| `EventWorkspaceFileChanged` | `workspace.file.changed` | Reserved (future) |

### Team Message Events
| Constant | Event Name |
|----------|-----------|
| `EventTeamMessageSent` | `team.message.sent` |

**Payload structs:** Typed payloads are defined in `pkg/protocol/team_events.go` (e.g., `TeamTaskEventPayload`, `DelegationEventPayload`, etc.).
