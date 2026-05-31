---
phase: 3
title: Validate workflow
status: completed
priority: P1
effort: 30m
dependencies:
  - 2
---

# Phase 3: Validate workflow

## Overview

Validate YAML syntax, dependency graph intent, and changed workflow blast radius.

## Requirements

- Functional: prove workflow file is syntactically valid and dependency graph matches acceptance.
- Non-functional: avoid running heavy Docker builds locally.

## Architecture

Local validation is static because GitHub-hosted runner context/secrets are required for full execution.

## Related Code Files

- Read: `.github/workflows/dev-beta-release.yaml`
- Read: `docs/deployment-guide.md`

## Implementation Steps

1. Parse workflow YAML with Ruby `YAML.safe_load` or available equivalent.
2. Inspect changed job `needs` and artifact names.
3. Add and run workflow-structure test for fast path, completion path, and stale guard.
4. Run lightweight repo checks that do not require services when feasible.
5. Ask tester/reviewer agents to validate regression risks.

## Success Criteria

- [ ] YAML parser accepts workflow.
- [ ] Static grep confirms deploy depends only on fast release publish.
- [ ] Node workflow-structure test passes.
- [ ] Review finds no blocker for release/deploy contract.

## Risk Assessment

Risk: static validation misses GitHub expression mistakes. Mitigation: keep expressions simple and reuse existing artifact/action patterns.
