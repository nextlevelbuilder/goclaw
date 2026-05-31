---
title: Issue 117 Agent Access Git Credential Follow-up
description: >-
  TDD follow-up for issue #117 after PR #120: collapse the confusing Agent
  Grants / Agent Credentials UI flow and fix git PAT / SSH credential validation
  so injected credentials work in real agent git commands.
status: completed
priority: P1
issue: 117
branch: codex/issue-117-agent-credentials-git-followup
tags: []
blockedBy: []
blocks: []
created: '2026-05-31T12:10:29.224Z'
createdBy: 'ck:plan'
source: skill
---

# Issue 117 Agent Access Git Credential Follow-up

## Overview

PR #120 added agent-scoped CLI credentials, but the follow-up evidence shows
two remaining defects:

- Web UI can mount Agent Grants and Agent Credentials independently, so admins
  can end up with overlapping modals for one binary.
- Production traces show agent credentials are selected and injected, yet git
  still fails: HTTPS clone ignores the PAT header shape, and SSH accepts a key
  at save time that OpenSSH later reports as `error in libcrypto`.

Approach:

1. Add failing characterization tests first for UI exclusivity and adapter
   behavior.
2. Replace the two competing agent actions with one Agent Access flow. Keep
   policy grants and typed secrets separate internally, but expose them as two
   views of the same operator task.
3. Change GitHub PAT injection to Basic auth extraheader and scrub the encoded
   secret form.
4. Validate SSH private keys against the OpenSSH parser before storing them, not
   only against Go's `x/crypto/ssh` parser.
5. Validate with focused Go and Web UI tests before git/ship.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [characterization-tests](./phase-01-characterization-tests.md) | Completed |
| 2 | [agent-access-ui](./phase-02-agent-access-ui.md) | Completed |
| 3 | [git-pat-auth](./phase-03-git-pat-auth.md) | Completed |
| 4 | [ssh-key-validation](./phase-04-ssh-key-validation.md) | Completed |
| 5 | [docs-ship](./phase-05-docs-ship.md) | Completed |

## Dependencies

- Prior completed plan: `plans/260531-1545-agent-scoped-git-credentials/`.
- Related issue: `digitopvn/goclaw#117`.
- Related merged PRs: `digitopvn/goclaw#120`, `digitopvn/goclaw#96`.
- Runtime evidence: production logs show `credential_source=agent` for both PAT
  and SSH paths, so this is an adapter/validation problem, not a missing
  credential lookup problem.

## Verified Facts

- `CliCredentialsPanel` currently tracks `agentCredsTarget` and `grantsTarget`
  as separate states and renders both dialogs if both are non-null.
- `gitAdapter.Prepare` currently writes
  `GIT_CONFIG_VALUE_0=Authorization: Bearer <token>` for PAT credentials.
- `docs/git-credential-adapter.md` already documents GitHub PAT as Basic auth,
  so code and docs diverged after PR #120.
- `prepareTypedCredentialEnv` validates SSH keys with `tools.ValidateSSHKey`
  before encryption, but that only exercises Go's parser.

## Out of Scope

- GitHub App installation tokens or OAuth device-code minting.
- Wildcard host scopes.
- Passphrase-protected SSH keys.
- Changing credential precedence. Existing precedence remains user > context >
  agent > binary env.
