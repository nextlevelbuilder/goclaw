---
phase: 2
title: agent-access-ui
status: completed
effort: M
---

# Phase 2: agent-access-ui

## Overview

Unify the operator task without merging policy and secret data models.
Admins should open one Agent Access dialog for a binary, then choose between
Credential and Access Policy inside that dialog.

## Implementation Steps

1. Replace `agentCredsTarget` and `grantsTarget` in
   `cli-credentials-panel.tsx` with one `agentAccessTarget` containing the
   binary and initial tab.
2. Add `cli-agent-access-dialog.tsx` as the single dialog root.
3. Extract non-dialog content from `CLIAgentCredentialsDialog` and
   `CliCredentialGrantsDialog` into reusable content components so the new
   dialog does not nest Radix dialogs.
4. Change table and grant chip actions to open Agent Access with either the
   credential or grants tab selected.
5. Add i18n keys for the Agent Access title, description, and tab labels in
   English, Vietnamese, and Chinese.

## Success Criteria

- [ ] At most one Radix dialog root is mounted for agent access from
      `CliCredentialsPanel`.
- [ ] The git row makes typed credential setup the primary Agent Access path.
- [ ] The grants view remains reachable from the same dialog.
- [ ] Existing User Credentials advanced override remains available.
