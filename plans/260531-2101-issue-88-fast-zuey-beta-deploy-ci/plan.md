---
title: Issue 88 fast zuey beta deploy CI
description: >-
  Refactor dev beta release workflow so zuey deploy starts after the linux amd64
  release asset is published, while arm64 and Docker artifacts still complete
  afterward.
status: completed
priority: P2
branch: codex/issue-88-fast-zuey-beta-deploy-ci
tags: []
blockedBy: []
blocks: []
created: '2026-05-31T14:01:31.321Z'
createdBy: 'ck:plan'
source: skill
---

# Issue 88 fast zuey beta deploy CI

## Overview

Issue #88 reports 30-40m dev beta CI. Recent run `26713221199` spent ~24m in Docker multi-arch while zuey deploy waited behind Docker promotion. Zuey deploy only needs the GitHub Release linux amd64 tarball, verified by `scripts/zuey/goclaw-upgrade-release.sh`.

Goal: optimize time-to-zuey deployed. Do not weaken Go/Web gates. Do not drop arm64, Docker `latest/full`, beta aliases, or checksums; only move them off the zuey critical path.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Scout and design](./phase-01-scout-and-design.md) | Completed |
| 2 | [Refactor dev beta workflow](./phase-02-refactor-dev-beta-workflow.md) | Completed |
| 3 | [Validate workflow](./phase-03-validate-workflow.md) | Completed |
| 4 | [Ship beta PR](./phase-04-ship-beta-pr.md) | Completed |

## Dependencies

- Base branch: `origin/dev`
- Primary files: `.github/workflows/dev-beta-release.yaml`, this plan
- GitHub issue: `digitopvn/goclaw#88`

## Acceptance Criteria

- `deploy_zuey_beta` no longer waits for `docker_images` or `promote_beta_aliases`.
- Workflow-level concurrency no longer serializes a new dev push behind slow artifact completion from the previous push.
- Zuey deploy and Docker beta alias promotion skip stale beta tags instead of rolling back to an older tag.
- The prerelease is published with `goclaw-${TAG}-linux-amd64.tar.gz` and `CHECKSUMS.sha256` before deploy.
- Linux arm64 binary, multi-arch Docker `latest/full`, and beta aliases still run after `beta_version` and keep workflow failure visible if broken.
- Workflow YAML parses and preserves repo guard `github.repository == 'digitopvn/goclaw'`.
- PR targets `dev` and links issue #88.
