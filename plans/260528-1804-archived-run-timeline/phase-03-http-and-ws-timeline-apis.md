---
phase: 3
title: "HTTP and WS Timeline APIs"
status: complete
priority: P1
effort: "1d"
dependencies: [1, 2]
---

# Phase 3: HTTP and WS Timeline APIs

## Context Links

- HTTP trace permissions reference: `internal/http/traces.go`
- WS chat methods reference: `internal/gateway/methods/chat.go`
- API docs: `docs/18-http-api.md`
- WS docs: `docs/19-websocket-rpc.md`

## Overview

Expose Phase 1 archive timeline through both HTTP and WebSocket RPC. User decision: both APIs are in scope for Phase 1.

## Key Insights

- HTTP is useful for direct UI fetch and future export.
- WS RPC keeps parity with existing dashboard control-plane patterns.
- Both paths must use one DTO mapper to avoid response drift.

## Requirements

- Functional: HTTP endpoint returns timeline by run ID.
- Functional: WS RPC returns the same DTO by run ID.
- Functional: support optional session key guard when provided.
- Functional: enforce tenant/user permissions; non-admin users can only access their own run/session timeline.
- Functional: include trace ID/span ID references for admin debug, but not full trace content.
- Non-functional: response shape must be stable for web UI and future exports.

## Architecture

HTTP route recommendation:

```text
GET /v1/runs/{runID}/timeline?session_key=<optional>
```

WS method recommendation:

```text
run.timeline.get
params: { runId: string, sessionKey?: string }
response: { runId, sessionKey, items: RunTimelineItem[] }
```

DTO should be shared between HTTP and WS methods to avoid drift.

## Related Code Files

- Create: `internal/http/run_timeline.go`
- Create: `internal/http/run_timeline_test.go`
- Create: `internal/gateway/methods/run_timeline.go`
- Create: `internal/gateway/methods/run_timeline_test.go`
- Modify: `internal/http/server.go` or route registration file
- Modify: `internal/gateway/methods/registry.go` or method registration file
- Modify: `pkg/protocol` only if method constants live there
- Modify: `ui/web/src/api/protocol.ts`
- Modify: `docs/18-http-api.md`
- Modify: `docs/19-websocket-rpc.md`

## Implementation Steps

1. Write HTTP auth/permission tests first:
   - admin can read tenant run timeline.
   - owner/user can read own run timeline.
   - other user receives not found/forbidden without leaking existence.
   - invalid run ID returns localized bad request.
2. Write WS RPC tests with equivalent permission cases.
3. Add shared response DTO and mapper.
4. Register HTTP route.
5. Register WS method.
6. Add docs for both APIs.
7. Confirm endpoint names do not conflict with existing `/v1/traces` routes.

## Success Criteria

- [ ] HTTP and WS return identical timeline item fields.
- [ ] Permission tests cover admin, owner, and unrelated user.
- [ ] Trace/span links are IDs only.
- [ ] API docs include request and response examples.
- [ ] No public share or export API is introduced in Phase 1.

## Todo List

- [ ] Add HTTP permission tests.
- [ ] Add WS permission tests.
- [ ] Add shared DTO mapper.
- [ ] Register HTTP route.
- [ ] Register WS method.
- [ ] Update HTTP and WS docs.

## Risk Assessment

Main risk: leaking run existence across tenants/users. Mitigation: scope all queries by tenant and session/user where available, and mirror trace handler non-admin behavior.

## Security Considerations

Return not found for cross-tenant or unauthorized access when possible. Do not include full trace/span payloads, only IDs.

## Next Steps

Proceed to Phase 4 after API parity tests pass.
