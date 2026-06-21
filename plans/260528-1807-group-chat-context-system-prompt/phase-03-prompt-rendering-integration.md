---
phase: 3
title: "Prompt Rendering Integration"
status: completed
priority: P1
effort: "3h"
dependencies: [1, 2]
---

# Phase 3: Prompt Rendering Integration

## Context Links

- Prompt config: `internal/agent/systemprompt.go`
- Loop message builder: `internal/agent/loop_history.go`
- Bridge prompt builder: `internal/agent/prompt_builder_impl.go`
- Prompt config types: `internal/agent/prompt_config_types.go`
- Consumer run request: `cmd/gateway_consumer_normal.go`

## Overview

Add explicit `## Current Chat Context` renderer and wire sender display name through prompt config. Preserve existing identity sentence, `<current_reply_target>`, group reply hint, and channel formatting hints.

## Key Insights

- `RunRequest.SenderName` already exists and is populated by `resolveSenderName(msg)`.
- `BuildSystemPrompt` receives `SenderID` but not sender display name.
- Chat context belongs in early dynamic identity prompt content.

## Requirements

- Functional: render approved block format.
- Functional: include group fields only for `PeerKind == "group"`.
- Functional: direct chat never shows group name or group ID.
- Non-functional: small helper, no template engine, no unrelated prompt reordering.

## Architecture

Add optional field:

```go
type SystemPromptConfig struct {
    SenderName string
}
```

Wire:

```text
RunRequest.SenderName
  -> buildMessages(...)
  -> SystemPromptConfig.SenderName
  -> buildCurrentChatContext(cfg)
```

Renderer rules:

- Platform: `ChannelType` fallback `Channel`.
- Chat type: `Group` for `PeerKind == "group"`, else `Direct`.
- Group name: sanitized `ChatTitle`, only if non-empty and group.
- Group ID: `ChatID`, only if non-empty and group.
- User: prefer sanitized `SenderName`; include `(ID: SenderID)` when `SenderID` non-empty.

## Related Code Files

- Modify: `internal/agent/systemprompt.go`
- Modify: `internal/agent/loop_history.go`
- Modify: `internal/agent/prompt_config_types.go`
- Modify: `internal/agent/prompt_builder_impl.go`
- Modify tests from Phase 1.

## Implementation Steps

1. Add `SenderName` to `SystemPromptConfig` and `IdentityData`.
2. Pass `SenderName` from `buildMessages` into `BuildSystemPrompt`.
3. Update `BridgePromptBuilder` to preserve field compatibility.
4. Implement `sanitizePromptContextValue` helper near existing identity logic.
5. Implement `buildCurrentChatContext(cfg)`.
6. Insert block after opening identity sentence and before `<current_reply_target>`.
7. Run prompt tests and adjust only if tests contradict approved format.

## Tests Before

- Phase 1 prompt tests fail.

## Refactor

- Extract existing title sanitation to avoid duplicate inline logic.
- Keep helper private to `systemprompt.go`.

## Tests After

- `go test ./internal/agent`
- `go test ./cmd`
- `go test ./internal/pipeline` if bridge tests require it.

## Todo List

- [x] Add `SenderName` config field.
- [x] Render block.
- [x] Preserve `<current_reply_target>`.
- [x] Preserve group reply hint.
- [x] Update bridge prompt tests if needed.

## Success Criteria

- [x] Prompt block matches approved format for group chats.
- [x] Existing prompt sections still present.
- [x] No provider-specific prompt regressions.
- [x] Direct chat behavior covered and intentional.

## Review Fixes

- Moved `## Current Chat Context` below `CacheBoundaryMarker` because sender identity is per-turn metadata.
- Added explicit untrusted-metadata wording so group titles and sender names are context only, never instructions.

## Risk Assessment

- Risk: token bloat. Mitigation: 4-5 short lines only.
- Risk: duplicate data with `<current_reply_target>`. Mitigation: block is human-readable; XML target remains routing guard.
- Risk: sender display unavailable. Mitigation: show user ID if name empty.

## Security Considerations

- Sanitize in renderer, not only adapters.
- Do not mention group-specific rules/memory; not implemented.

## Next Steps

- Phase 4 verifies build/test and publishes issue handoff.
