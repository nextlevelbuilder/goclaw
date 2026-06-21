---
title: Agent-scoped Git credentials
description: >-
  TDD plan for moving git typed credentials from channel/user-id keyed defaults
  to agent-scoped credentials, with HTTP API and Web UI management.
status: completed
priority: P1
issue: 117
branch: codex/issue-117-agent-scoped-git-credentials-plan
tags: []
blockedBy: []
blocks: []
created: '2026-05-31T08:45:33.071Z'
createdBy: 'ck:plan'
source: skill
---

# Agent-scoped Git credentials

## Overview

Issue #117 started as a UI gap: the git template does not make it obvious where to enter `GH_PAT` or SSH key material. The deeper design problem is that current `User Credentials` are keyed by credential user ID, while the same human can appear as different external IDs across Discord, Telegram, HTTP, or group contexts.

Decision: make agent-scoped git credentials the primary model. Granting access to an agent becomes the security boundary for whether a user can cause that agent to use a git PAT or SSH key. Keep per-user credentials as an advanced override for backward compatibility and truly personal credentials.

TDD target:

- Add contract tests first for effective credential precedence and API behavior.
- Add a dedicated agent credential storage surface instead of mixing typed secrets into agent grant policy rows.
- Add HTTP endpoints for create, edit, list, detail, and delete of agent-scoped CLI credentials.
- Update Web UI so git PAT and SSH setup is managed from Agent Credentials by default.
- Validate runtime injection across Discord/Telegram/userless contexts uses the same agent credential.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Research and contract tests](./phase-01-research-and-contract-tests.md) | Completed |
| 2 | [Schema and store resolver](./phase-02-schema-and-store-resolver.md) | Completed |
| 3 | [HTTP API credential management](./phase-03-http-api-credential-management.md) | Completed |
| 4 | [Web UI credential management](./phase-04-web-ui-credential-management.md) | Completed |
| 5 | [Runtime git adapter validation](./phase-05-runtime-git-adapter-validation.md) | Completed |
| 6 | [Docs validation and handoff](./phase-06-docs-validation-and-handoff.md) | Completed |

## Dependencies

- Current typed git adapter and validation: `internal/tools/credential_adapter_git.go`, `internal/http/secure_cli_typed_credentials.go`.
- Current lookup joins per-user credentials in `internal/store/pg/secure_cli.go` and SQLite equivalent.
- Current UI git form lives under `ui/web/src/pages/cli-credentials/cli-user-credentials-dialog.tsx`.
- Migration number must be re-verified at implementation time. As of plan creation, latest PostgreSQL migration is `000076_channel_memory_extraction`.

## Verified Facts

- `LookupByBinary` takes `(binaryName, agentID, userID)` and only joins `secure_cli_user_credentials` when `userID` is non-empty.
- Context credentials can currently override/fill credential fields via `applyContextSecureCLI`, but there is no agent credential typed secret row.
- `SecureCLIAgentGrant` already has `encrypted_env`, but lacks `credential_type` and `host_scope`; using it for typed git secrets would mix policy and secret identity.
- HTTP routes currently expose `/v1/cli-credentials/{id}/user-credentials...` but not agent credential endpoints.
- The Web UI currently opens User Credentials from the CLI credentials table action.

## Out of Scope

- GitHub App installation tokens.
- OAuth/device-code token minting.
- Wildcard host scopes.
- Passphrase-protected SSH keys.
- Sandbox-mode git credential injection.
