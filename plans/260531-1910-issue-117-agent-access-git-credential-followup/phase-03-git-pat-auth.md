---
phase: 3
title: git-pat-auth
status: completed
effort: S
---

# Phase 3: git-pat-auth

## Overview

Fix HTTPS git auth for GitHub PAT credentials.

## Implementation Steps

1. In `internal/tools/credential_adapter_git.go`, encode PAT credentials as
   `Authorization: Basic base64("x-access-token:<token>")` for
   `http.https://<host>/.extraheader`.
2. Preserve host-scoped injection and existing command allowlist behavior.
3. Add both raw token and base64/header forms to scrub values.
4. Update adapter tests and any integration helpers that describe the git HTTP
   auth mode.

## Success Criteria

- [ ] PAT tests assert Basic auth header, not Bearer.
- [ ] Encoded PAT material cannot appear unsanitized in credentialed exec logs.
- [ ] SSH adapter behavior is unchanged by this phase.
