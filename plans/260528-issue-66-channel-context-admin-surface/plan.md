---
title: "Issue 66 Channel Context Admin Surface"
description: "Add channel details admin views for contexts, members, granted MCP/CLI capabilities, and scoped credential overrides."
status: complete
priority: P2
issue: 66
branch: "codex-issue-66-channel-details-admin-design"
tags: [channels, web-ui, security, tdd, issue-66]
blockedBy: []
blocks: []
created: "2026-05-28T11:49:55.757Z"
createdBy: "ck:plan"
source: skill
---

# Issue 66 Channel Context Admin Surface

## Overview

Implement issue #66 as an incremental channel-context admin surface, not a one-shot RBAC rewrite.

The first usable milestone is read-only: channel groups/contexts, members/users, and effective MCP/CLI capability visibility. Mutations come after contracts and tests prove tenant isolation, masking, audit logging, and resolver precedence.

Credential precedence is: user > group/member-role > channel > agent > global. This keeps user credentials most specific and avoids shared channel secrets overriding a user's explicit credential.

Out of scope for the first implementation pass: raw credential reveal, storing MCP/CLI secrets in `channel_instances.credentials`, and full Discord role/member sync without validating required Discord intents and permissions.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Research and Contracts](./phase-01-research-and-contracts.md) | Complete |
| 2 | [Read-only Context Surface](./phase-02-read-only-context-surface.md) | Complete |
| 3 | [Effective Capability Matrix](./phase-03-effective-capability-matrix.md) | Complete |
| 4 | [Context-scoped Grants](./phase-04-context-scoped-grants.md) | Complete |
| 5 | [Context-scoped Credentials](./phase-05-context-scoped-credentials.md) | Complete |
| 6 | [Discord Enrichment and Verification](./phase-06-discord-enrichment-and-verification.md) | Complete: live role/member enrichment remains gated behind Discord intent verification |

## Dependencies

- GitHub issue: https://github.com/digitopvn/goclaw/issues/66
- Existing web UI: `ui/web/src/pages/channels/channel-detail/`
- Existing HTTP routes: `internal/http/channel_instances.go`, `internal/http/mcp_grants.go`, `internal/http/secure_cli_agent_grants.go`
- Existing stores: `internal/store/pg/`, `internal/store/sqlitestore/`, `internal/store/*mcp*`, `internal/store/*secure_cli*`
- Migration rule: PostgreSQL migrations and SQLite schema/migration map must stay in sync.
- Verification baseline: focused Go tests first, then `go build ./...`, `go build -tags sqliteonly ./...`, and relevant web checks with `pnpm` in `ui/web/`.

## Unresolved Questions

None. Discord live role/member enrichment is intentionally not enabled until required intents and permissions are verified.
