# Bug Report: Group File Writer Permission Denied in Telegram Group Chat

## Summary

In Telegram **group chats**, the agent is blocked from using file manipulation tools (`write_file`, `edit`, `read_file` on protected files) even after a user has been granted writer access via `/addwriter`. The permission check fails silently due to a **cache key architecture mismatch** between how writer grants are stored and how they are looked up at tool execution time.

---

## Environment

- **Platform:** Telegram (Group Chat)
- **Affected versions:** v3.6.0, v3.7.1 (and current `dev` branch at commit `a8c01dbc`)
- **Reproduced on:** Fox Spirit agent (predefined agent type)
- **Related issue:** [#915](https://github.com/nextlevelbuilder/goclaw/issues/915)

---

## Affected Tools

| Tool | Entry point | Permission call |
|------|-------------|-----------------|
| `write_file` | `internal/tools/filesystem_write.go` | `store.CheckFileWriterPermission()` |
| `edit` | `internal/tools/edit.go` | `store.CheckFileWriterPermission()` |
| `read_file` (SOUL.md, AGENTS.md) | `internal/tools/filesystem.go` | `store.CheckFileWriterPermission()` |
| Context file writes (SOUL.md, USER.md, etc.) | `internal/tools/context_file_interceptor.go` | `permStore.CheckPermission()` directly |

---

## Error Message

```
permission denied: only file writers can modify files in this group. Use /addwriter to get write access
```

This message is produced in **two separate code paths**:

1. `internal/store/config_permission_store.go` → `CheckFileWriterPermission()` — hit by `write_file`, `edit`, `read_file`
2. `internal/tools/context_file_interceptor.go` → `WriteFile()` — hit by context file writes

---

## Full Message Flow (Telegram Group Chat)

```
User sends message in Telegram group
  ↓
internal/channels/telegram/handlers.go
  senderID = "123456"          (Telegram numeric user ID)
  userID   = "123456"          (same at this point)
  peerKind = "group"
  ↓
bus.InboundMessage published:
  SenderID = "123456"
  UserID   = "123456"
  ChatID   = "-1001234567"
  Channel  = "telegram"
  ↓
cmd/gateway_consumer_normal.go (lines 89–98)
  → userID overwritten to group-scoped:
  userID = fmt.Sprintf("group:%s:%s", msg.Channel, msg.ChatID)
         = "group:telegram:-1001234567"
  SenderID preserved = "123456"
  ↓
agent.RunRequest{
  UserID:   "group:telegram:-1001234567",  ← scope
  SenderID: "123456",                       ← individual sender
}
  ↓
internal/agent/loop_context.go
  ctx = store.WithUserID(ctx, "group:telegram:-1001234567")
  ctx = store.WithSenderID(ctx, "123456")
  ↓
Tool Execute(ctx) called
  ↓
store.CheckFileWriterPermission(ctx, permStore)
  userID   = "group:telegram:-1001234567"   ← scope for lookup
  senderID = "123456"                        ← user to check
  numericID = "123456"
  → calls permStore.CheckPermission(ctx, agentID, "group:telegram:-1001234567", "file_writer", "123456")
  → FAILS (returns false)
  → returns error: "permission denied..."
```

---

## Root Cause

The bug is in `internal/store/config_permission_store.go` → `CheckFileWriterPermission()`, which delegates to `CheckPermission()`.

### `CheckPermission` — how it caches (the broken path)

```go
// internal/store/pg/config_permissions.go
cacheKey := tid.String() + ":" + agentID.String() + ":" + userID
//                                                          ↑ this is senderID ("123456")
```

The DB query fetches **all permission rows for that sender**:
```sql
SELECT scope, config_type, permission, user_id
FROM agent_config_permissions
WHERE agent_id = $1 AND (user_id = $2 OR user_id = '*')
-- $2 = "123456"
```

Then `evalPermRows()` filters those rows against `scope = "group:telegram:-1001234567"`.

**The problem:** If the sender has never interacted in a DM context before, the cache is cold. The DB query runs and correctly returns the grant row. But if the cache was **previously populated** in a different scope context (e.g., DM), the cached rows may not include the group-scoped grant row, causing `evalPermRows` to return `false` → deny.

Additionally, this path uses `CheckPermission` which has a TTL of **60 seconds** — if the grant was just added via `/addwriter`, the stale cache from before the grant will deny the request for up to 60 seconds.

### `ListFileWriters` — how it caches (the correct path)

```go
// internal/store/pg/config_permissions.go
cacheKey := tid.String() + ":" + agentID.String() + ":" + scope
//                                                          ↑ this is groupID ("group:telegram:-1001234567")
```

The DB query fetches **all writer rows for that group scope**:
```sql
SELECT ... FROM agent_config_permissions
WHERE agent_id = $1 AND scope = $2 AND config_type = 'file_writer' AND permission = 'allow'
-- $2 = "group:telegram:-1001234567"
```

This is **exactly how `/addwriter` checks writers** in `commands_writers.go`:
```go
existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)
```

The grant lookup and the permission check use the **same cache key** and the **same query pattern** — guaranteed consistency.

---

## Code Locations

### Where the grant is stored (`/addwriter`)

```
internal/channels/telegram/commands_writers.go:44
  groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)
  // stored as: scope="group:telegram:-1001234567", user_id="123456"
```

### Where the permission is checked (broken)

```
internal/store/config_permission_store.go:52
  func CheckFileWriterPermission(ctx, permStore) error
    → permStore.CheckPermission(ctx, agentID, userID, ConfigTypeFileWriter, numericID)
    → cache key: "tid:agentID:123456"  ← keyed by sender, not by group scope
```

### Where `CheckFileWriterPermission` is called

```
internal/tools/filesystem_write.go   WriteFileTool.Execute()
internal/tools/edit.go               EditTool.Execute()
internal/tools/filesystem.go         ReadFileTool.Execute() (for SOUL.md, AGENTS.md only)
```

### Secondary check location (context files only)

```
internal/tools/context_file_interceptor.go:222
  b.permStore.CheckPermission(ctx, agentID, userID, store.ConfigTypeFileWriter, numericID)
  → same broken pattern, called directly (not via CheckFileWriterPermission)
```

---

## Proposed Fix

### Fix 1 — `internal/store/config_permission_store.go` (covers write_file, edit, read_file)

Replace the body of `CheckFileWriterPermission` to use `ListFileWriters` instead of `CheckPermission`:

```go
func CheckFileWriterPermission(ctx context.Context, permStore ConfigPermissionStore) error {
    if permStore == nil {
        return nil
    }
    userID := UserIDFromContext(ctx)
    if !strings.HasPrefix(userID, "group:") && !strings.HasPrefix(userID, "guild:") {
        return nil
    }
    agentID := AgentIDFromContext(ctx)
    if agentID == uuid.Nil {
        return nil
    }
    senderID := SenderIDFromContext(ctx)
    if senderID == "" {
        return nil
    }
    numericID := strings.SplitN(senderID, "|", 2)[0]

    writers, err := permStore.ListFileWriters(ctx, agentID, userID)
    if err != nil {
        return nil // fail-open
    }
    for _, w := range writers {
        if w.UserID == numericID && w.Permission == "allow" {
            return nil
        }
    }
    return fmt.Errorf("permission denied: only file writers can modify files in this group. Use /addwriter to get write access")
}
```

### Fix 2 — `internal/tools/context_file_interceptor.go` (covers context file writes)

Replace the `CheckPermission` call inside `WriteFile()` with `ListFileWriters` loop (same pattern as Fix 1).

---

## Why Existing Tests Failed

The existing test stubs for `ConfigPermissionStore` implement `CheckPermission` to return `(false, nil)` by default. When `CheckFileWriterPermission` was changed to use `ListFileWriters`, the test stubs needed to return populated `writers` slices from `ListFileWriters`. The stubs were not updated, causing all permission-related tests to break.

**The fix requires updating test stubs** to populate `ListFileWriters` return values alongside the logic change.

---

## Verification SQL

After `/addwriter`, confirm the grant exists correctly:

```sql
SELECT scope, config_type, user_id, permission, created_at
FROM agent_config_permissions
WHERE config_type = 'file_writer'
ORDER BY created_at DESC
LIMIT 10;
```

Expected row:
```
scope                          | config_type  | user_id | permission
group:telegram:-1001234567     | file_writer  | 123456  | allow
```

If `scope` or `user_id` does not match exactly what the tool runtime uses, that confirms the mismatch.

---

## Files Involved

| File | Role |
|------|------|
| `internal/store/config_permission_store.go` | **Primary fix location** — `CheckFileWriterPermission` |
| `internal/tools/context_file_interceptor.go` | Secondary fix — `WriteFile` direct `CheckPermission` call |
| `internal/tools/filesystem_write.go` | Calls `CheckFileWriterPermission` |
| `internal/tools/edit.go` | Calls `CheckFileWriterPermission` |
| `internal/tools/filesystem.go` | Calls `CheckFileWriterPermission` |
| `internal/channels/telegram/commands_writers.go` | Where grants are stored — uses `ListFileWriters` correctly |
| `internal/store/pg/config_permissions.go` | Cache implementation — two separate caches with different keys |
| `cmd/gateway_consumer_normal.go` | Where `userID` is rewritten to group-scoped |
| `internal/agent/loop_context.go` | Where `SenderID` and `UserID` are injected into context |
| `internal/tools/context_file_interceptor_test.go` | Test stubs need updating when fix is applied |