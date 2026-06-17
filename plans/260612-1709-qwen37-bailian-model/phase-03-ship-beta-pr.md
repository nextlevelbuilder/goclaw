---
phase: 3
title: "Ship beta PR"
status: pending
priority: P2
effort: "30m"
dependencies: [2]
---

# Phase 3: Ship beta PR

## Overview

Ship the completed issue #169 work as a beta-targeted PR to `dev`, with labels updated according to `ck:vibe --beta`.

## Requirements

- Functional: GitHub issue gets a plan/update comment and `ready to cook` before implementation.
- Functional: PR targets `dev` and references issue #169.
- Functional: after local and PR review gates pass, source issue and PR get `ready to ship beta`.
- Non-functional: no merge is performed because the user invoked `--beta`, not `--ship --beta`.

## Implementation Steps

1. Ensure GitHub labels exist: `ready to cook`, `ready to ship stable`, `ready to ship beta`.
2. Comment on issue #169 with branch, relative plan path, beta mode, and acceptance criteria.
3. After implementation/tests/review, commit with a focused conventional message.
4. Push `codex/issue-169-qwen37-bailian-model`.
5. Create beta PR against `dev`.
6. Review/fix/reply PR feedback and wait for terminal checks when available.
7. Add `ready to ship beta` to both issue and PR; remove `ready to cook`.

## Success Criteria

- [ ] PR exists and targets `dev`.
- [ ] PR/issue labels reflect ready-to-ship-beta state after review.
- [ ] Merge is skipped.

## Risk Assessment

Risk: GitHub auth lacks label/PR permissions.
Mitigation: stop with exact `gh` error if label or PR creation fails.

## Security Considerations

Do not include secrets or private env output in GitHub comments/PR body.
