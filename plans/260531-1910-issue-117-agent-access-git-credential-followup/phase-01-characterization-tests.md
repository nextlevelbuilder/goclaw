---
phase: 1
title: characterization-tests
status: completed
effort: S
---

# Phase 1: characterization-tests

## Overview

Write tests that describe the broken behavior before implementation.

## Implementation Steps

1. Add a Web UI wiring test proving `CliCredentialsPanel` exposes one agent
   access surface instead of mounting separate Agent Grants and Agent
   Credentials dialogs from independent state.
2. Update git adapter PAT tests to expect GitHub-compatible Basic auth
   extraheader and to assert both raw and encoded token material are scrubbed.
3. Add SSH storage-validation tests proving save-time validation calls an
   OpenSSH compatibility check after Go parser validation.
4. Run the new tests first and keep the initial failing output in the
   implementation notes.

## Success Criteria

- [ ] UI test fails on current code because two agent dialog targets are still
      independent.
- [ ] PAT test fails on current code because it still emits Bearer auth.
- [ ] SSH validation test fails on current code because no OpenSSH
      compatibility check is called at storage time.
- [ ] No production secrets or trace payload content are copied into tests.
