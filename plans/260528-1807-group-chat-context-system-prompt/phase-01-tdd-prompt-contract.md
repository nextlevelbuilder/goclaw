---
phase: 1
title: "TDD Prompt Contract"
status: completed
priority: P1
effort: "2h"
dependencies: []
---

# Phase 1: TDD Prompt Contract

## Context Links

- Plan: `plans/260528-1807-group-chat-context-system-prompt/plan.md`
- Issue: `digitopvn/goclaw#70`
- Prompt builder: `internal/agent/systemprompt.go`
- Prompt tests: `internal/agent/systemprompt_*_test.go`

## Overview

Define and lock expected prompt output before implementation. Prove exact block shape for group, direct, missing-title, and injection-shaped metadata cases.

## Key Insights

- Existing prompt already has `ChatTitle`, `ChatID`, and `PeerKind`.
- Data is split between an identity sentence and `<current_reply_target>`.
- User approved a clear block format, not just extending `<current_reply_target>`.
- Group title is admin-controlled input. Sanitize before prompt output.

## Requirements

- Functional: failing tests for `## Current Chat Context`.
- Functional: group prompt includes platform, chat type, group name when known, group ID, user display/name and sender ID.
- Functional: direct prompt does not expose group fields.
- Non-functional: no channel or DB changes in this phase.

## Architecture

Test `BuildSystemPrompt(SystemPromptConfig)` directly with small string assertions.

Expected group block:

```text
## Current Chat Context
- Platform: telegram
- Chat type: Group
- Group name: GoClaw Contributors
- Group ID: -1001234567890
- User: Alice (ID: 123456)
```

## Related Code Files

- Modify: `internal/agent/systemprompt.go` only if compile requires new config field.
- Modify or create: `internal/agent/systemprompt_chat_context_test.go`.

## Implementation Steps

1. Add failing test for group prompt with title and sender display name.
2. Add failing test for group prompt without title; assert no `Group name:` line.
3. Add failing test for direct chat; assert no `Group ID` or `Group name`.
4. Add failing test for sanitization: newlines and quotes in title/user name stripped/replaced and capped.
5. Keep assertions on relevant block lines, not full prompt.

## Tests Before

- `go test ./internal/agent -run 'ChatContext|CurrentChatContext'`
- Expected before implementation: fail because block does not exist.

## Refactor

- None except adding minimal config field if tests need it.

## Tests After

- Same targeted tests still fail until Phase 3 implements renderer.
- No unrelated test rewrites.

## Todo List

- [x] Add prompt contract tests.
- [x] Verify tests fail for missing block.
- [x] Document exact expected block in test names.

## Success Criteria

- [x] Failing tests cover group with title, group without title, direct chat, sanitization.
- [x] Test names describe scenario, not plan labels.
- [x] No implementation logic added before failing tests exist.

## Risk Assessment

- Risk: brittle full-prompt tests. Mitigation: assert only relevant block lines.
- Risk: user name not available in config. Mitigation: add optional `SenderName` in Phase 3.

## Security Considerations

- Treat group name and user display name as untrusted prompt content.
- Sanitize newline, carriage return, quotes, and long text.

## Next Steps

- Phase 2 normalizes channel metadata so prompt input has best-effort group names.
