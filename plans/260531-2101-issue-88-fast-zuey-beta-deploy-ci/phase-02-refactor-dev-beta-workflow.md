---
phase: 2
title: Refactor dev beta workflow
status: completed
priority: P1
effort: 1h
dependencies:
  - 1
---

# Phase 2: Refactor dev beta workflow

## Overview

Refactor `.github/workflows/dev-beta-release.yaml` so the release needed by zuey is published before slow Docker multi-arch jobs finish.

## Requirements

- Functional: split amd64 release publishing from full artifact completion.
- Non-functional: keep all current artifact families and registry publishing.

## Architecture

Use two binary jobs:
- `build_zuey_binary`: linux amd64 only, feeds the first prerelease publish.
- `build_remaining_binaries`: linux arm64, feeds final release artifact completion.

Keep Docker jobs parallel after `beta_version`. Keep `promote_beta_aliases` after Docker. Add a release completion job that re-uploads all binary assets and full `CHECKSUMS.sha256`.

Move from workflow-level concurrency to job-level concurrency only where mutation ordering matters. Add stale beta tag guards before zuey deploy and beta alias promotion.

## Related Code Files

- Modify: `.github/workflows/dev-beta-release.yaml`
- Modify: `docs/deployment-guide.md`
- Modify: `docs/project-changelog.md`
- Create: `scripts/ci/dev-beta-release-workflow.test.mjs`
- Delete: none

## Implementation Steps

1. Rename/split existing `build_binaries` into amd64 fast job and remaining arch job.
2. Change `publish_release.needs` to `[beta_version, build_zuey_binary]`.
3. Download only `binary-linux-amd64` in `publish_release`.
4. Add `complete_release_artifacts` needing `[beta_version, publish_release, build_zuey_binary, build_remaining_binaries]`.
5. Keep `docker_images` and `promote_beta_aliases` unchanged except not gating deploy.
6. Keep `deploy_zuey_beta.needs` as `[beta_version, publish_release]`.
7. Remove top-level workflow concurrency; add job-level concurrency and stale-tag guard for deploy/alias mutation jobs.
8. Update deployment docs and changelog for the new order.

## Success Criteria

- [ ] `deploy_zuey_beta` critical path excludes Docker jobs.
- [ ] Full release upload still includes amd64 + arm64 tarballs and refreshed checksum.
- [ ] Docker beta aliases still promote after multi-arch images.
- [ ] Stale beta deploy/alias promotion is skipped.
- [ ] No secret, branch, or repository guard is weakened.

## Risk Assessment

Risk: two jobs uploading to same release can race. Mitigation: `complete_release_artifacts` depends on `publish_release` and uses `gh release upload --clobber` after the fast publish.
