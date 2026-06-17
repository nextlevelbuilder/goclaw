---
phase: 4
title: Ship beta PR
status: completed
priority: P2
effort: 30m
dependencies:
  - 3
---

# Phase 4: Ship beta PR

## Overview

Ship the workflow refactor to `dev` via PR and preserve beta release observability.

## Requirements

- Functional: commit, push branch, create PR into `dev`, link issue #88.
- Non-functional: do not merge automatically; beta workflow runs after PR merge to `dev`.

## Architecture

Use normal PR shipping. After merge, the first `dev` push is the real validation for zuey time-to-deploy and full artifact completion.

## Related Code Files

- Commit: `.github/workflows/dev-beta-release.yaml`
- Commit: `plans/260531-2101-issue-88-fast-zuey-beta-deploy-ci/*`

## Implementation Steps

1. Run final `git diff --check` and workflow validation.
2. Commit with conventional message.
3. Push `codex/issue-88-fast-zuey-beta-deploy-ci`.
4. Create PR to `dev`, mention `Fixes #88`.
5. Report post-merge watch command for first real beta run.

## Success Criteria

- [ ] Branch pushed to origin.
- [ ] PR opened against `dev`.
- [ ] PR body explains fast path and artifact completion path.

## Risk Assessment

Risk: `dev` workflow behavior cannot be fully proven before merge. Mitigation: PR includes expected job-order evidence and first-run watch steps.
