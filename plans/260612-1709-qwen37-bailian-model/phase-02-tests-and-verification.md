---
phase: 2
title: Tests and verification
status: completed
priority: P2
effort: 30m
dependencies:
  - 1
---

# Phase 2: Tests and verification

## Overview

Add regression coverage for Bailian model discovery and run focused validation for the touched backend/docs paths.

## Requirements

- Functional: endpoint-level test proves Bailian returns `qwen3.7-plus`.
- Functional: test verifies no unsupported reasoning metadata is exposed for this OpenAI-compatible Bailian model.
- Non-functional: targeted Go package tests pass with the installed Go binary.

## Implementation Steps

1. Add a provider models test in `internal/http/provider_models_test.go` for `store.ProviderBailian`.
2. Use existing mock provider store and auth token helpers.
3. Assert HTTP 200, model ID/display name, and nil `Reasoning` for `qwen3.7-plus`.
4. Run focused test:
   - `PATH=/usr/local/go/bin:$PATH go test ./internal/http -run 'Bailian|ProviderModels' -count=1`
5. Run compile checks when feasible:
   - `PATH=/usr/local/go/bin:$PATH go test ./internal/http -count=1`
   - broader build/test if the focused package uncovers shared changes.

## Success Criteria

- [ ] Focused test fails before catalog change and passes after.
- [ ] `go test ./internal/http` passes.
- [ ] No generated or unrelated files changed.

## Risk Assessment

Risk: test helper requires an API key for Bailian before returning catalog.
Mitigation: set a dummy API key on the mock provider because the handler checks non-empty `APIKey` before dispatch.

## Security Considerations

Use dummy values only; do not read or write env/secrets.
