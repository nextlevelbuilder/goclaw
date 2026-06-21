# AI Agent Permission Matrix

This matrix documents the effective authorization layers for agent actions across channels, groups, and workspaces.

## Permission Layers

| Layer | Scope | Enforced By | Notes |
|-------|-------|-------------|-------|
| Tenant RBAC | Dashboard, HTTP, WebSocket RPC | `internal/permissions` | Viewer/operator/admin/owner. Admin methods include `config.permissions.*`. |
| Agent ownership/share | Agent visibility and management | `store.AgentStore.CanAccess` | Controls which agents a dashboard user can manage. |
| Channel membership | Platform delivery | Channel adapter | Platform can still reject outbound delivery after GoClaw allows it. |
| Agent config permissions | Agent config mutations from chat | `agent_config_permissions` | Matches by `agent_id`, `scope`, `config_type`, `user_id`, including wildcard rows. |
| Workspace file boundary | Filesystem access | tool sandbox/boundary checks | Prevents path escape and unsupported writes. |
| Context file boundary | Agent identity/context files | `ContextFileInterceptor` | Routes protected files to store and requires group writer permission in group contexts. |
| Channel context capabilities | MCP + Secure CLI tool execution | `mcp_context_*`, `secure_cli_context_*` | Effective precedence: user credentials > context credentials/grants > agent grants > global defaults. |

## Agent Config Permission Rows

| Field | Examples | Meaning |
|-------|----------|---------|
| `scope` | `agent`, `group:*`, `group:zalo:123`, `group:telegram:-100`, `*` | Where the grant applies. |
| `config_type` | `file_writer`, `heartbeat`, `cron`, `context_files`, `*` | What action family the grant covers. |
| `user_id` | `123456`, `zalo-user-id`, `*` | Who the grant covers. `*` grants every member in the selected scope. |
| `permission` | `allow`, `deny` | Effective decision. Deny can override broader allow. |

Effective precedence:

1. Individual deny.
2. Individual allow.
3. Scope/user wildcard deny.
4. Scope/user wildcard allow.
5. Default deny.

## Channel Matrix

| Channel Context | Read Agent Output | Send Reply | Write Workspace File | Write Protected Context File | Grant All Members |
|-----------------|-------------------|------------|----------------------|------------------------------|-------------------|
| Dashboard | RBAC controlled | N/A | Admin/operator path, then workspace boundary | Admin path, then context interceptor | Use Permissions tab |
| Direct message | Agent/session access | Channel adapter | Allowed by workspace boundary | Allowed by agent/context rules | Usually not needed |
| Telegram group | Group scope + sender ID | Channel adapter | Requires `file_writer` when group-gated | Requires `context_files` or `file_writer` and real sender | `scope=group:telegram:<chatId>`, `user_id=*` |
| Zalo group | Group scope + sender ID | Channel adapter, group thread metadata | Requires `file_writer` when group-gated | Requires `context_files` or `file_writer` and real sender | `scope=group:zalo:<chatId>`, `user_id=*` |
| Discord guild/channel | Guild scope + sender ID | Channel adapter | Requires `file_writer` when guild-gated | Requires `context_files` or `file_writer` and real sender | `scope=guild:<id>` or matching group scope, `user_id=*` |
| Scheduled/proactive run | System sender | Channel adapter | Deny for group-gated file writes unless elevated context | Deny for protected group context writes | Configure explicit rules or run from dashboard/admin context |

## Zalo Context Write Rule

Zalo group failures commonly happen when an agent writes `SOUL.md`, `IDENTITY.md`, `AGENTS.md`, `USER.md`, `USER_PREDEFINED.md`, or `CAPABILITIES.md` from a group session but the acting sender is missing. Protected context writes now use the group permission gate:

- `sender_id` must be a real platform user, not empty or synthetic.
- `user_id` must identify the group scope, for example `group:zalo:<chatId>`.
- The sender must match a `context_files` allow or legacy `file_writer` allow, including wildcard rows such as `user_id="*"`.
- Missing tenant context or permission-store errors fail closed.

## UX Contract

The Permissions tab should expose a full matrix editor:

| Control | Behavior |
|---------|----------|
| User/contact picker | Accepts explicit user IDs and contact search results. |
| All members button | Sets `user_id="*"` for the current rule. |
| Config type selector | Supports `file_writer`, `heartbeat`, `cron`, `context_files`, and `*`. |
| Scope selector | Supports known groups, `group:*`, `agent`, and `*`. |
| Check access | Calls `config.permissions.check` and shows the effective allow/deny decision before or after saving. |

## Security Notes

- Wildcard `user_id="*"` should be easy to grant but visually explicit because it expands access to every member in scope.
- Synthetic senders remain denied for group file/context writes. This avoids system turns inheriting permissions from no real user.
- Permission-store errors fail closed for group mutation boundaries.
- Backend validation rejects unknown config types and permissions before writing rules.
- Platform send permissions are still separate from GoClaw permissions; a channel adapter may reject delivery even when GoClaw allows the agent action.

## Channel context capabilities

Channel instances expose stored contexts in the dashboard and API. The base
context is the channel instance itself; group contexts come from stored channel
contacts. Capability rows combine MCP and Secure CLI visibility for that
context, including source, enabled state, tool allow/deny lists, and credential
presence.

Context credential rows never return secret material. They only project
metadata such as `has_api_key`, `has_env`, `credential_source`, and key names
where available. Writes are tenant-admin gated, and runtime resolution carries
`ChannelContextScope` so grants and credentials are applied only to matching
channel/group scope.
