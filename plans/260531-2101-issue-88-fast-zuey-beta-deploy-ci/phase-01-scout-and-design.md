---
phase: 1
title: Scout and design
status: completed
priority: P1
effort: 30m
dependencies: []
---

# Phase 1: Scout and design

## Overview

Verify current workflow graph, zuey asset contract, and overlapping plans before editing CI.

## Requirements

- Functional: document the exact current blockers for zuey deploy.
- Non-functional: plan must be grounded in live workflow code and recent run timing.

## Architecture

Current path: `go/web -> beta_version -> build_binaries + docker_images -> promote_beta_aliases -> publish_release -> deploy_zuey_beta`.

Fast path target: `go/web -> beta_version -> build_zuey_binary -> publish_release -> deploy_zuey_beta`.

## Implementation Steps

1. Read `README.md`, `CLAUDE.md`, `.github/workflows/dev-beta-release.yaml`, and `scripts/zuey/goclaw-upgrade-release.sh`.
2. Check issue #88 and recent workflow job timings with `gh`.
3. Check unfinished plans for overlapping CI release work.
4. Verify zuey only requires linux amd64 release tarball and checksum/digest.

## Success Criteria

- [ ] Workflow dependency bottleneck identified with file/line refs.
- [ ] Zuey asset requirement identified with file/line refs.
- [ ] No overlapping active plan blocks this work.

## Risk Assessment

Risk: publishing partial prerelease could confuse consumers. Mitigation: follow-up job refreshes release assets/checksums and workflow stays failed if completion breaks.
