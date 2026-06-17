---
phase: 1
title: Catalog and capability docs
status: completed
priority: P2
effort: 30m
dependencies: []
---

# Phase 1: Catalog and capability docs

## Overview

Add the new Bailian model to the backend catalog and document its advertised capabilities without changing runtime request-body semantics.

## Requirements

- Functional: `bailianModels()` returns `{ID: "qwen3.7-plus", Name: "Qwen 3.7 Plus"}`.
- Functional: docs state the model capabilities from issue #169: Text Generation, Deep Thinking, Visual Understanding.
- Non-functional: no new API schema fields unless already supported by `ModelInfo`.
- Non-functional: no runtime behavior change for existing Bailian models.

## Architecture

`GET /v1/providers/{id}/models` loads a provider from the store, dispatches `provider_type == "bailian"` to `bailianModels()`, and returns the list. Web and desktop provider model pickers already consume this endpoint, so the backend catalog update is the UI propagation path.

Do not add `qwen3.7-plus` to `internal/providers/reasoning_capability.go`: `OpenAIProvider.buildRequestBody()` uses that registry to decide whether to send OpenAI `reasoning_effort`. Bailian is registered as `NewOpenAIProvider(...)`, not `NewDashScopeProvider(...)`, so Qwen Deep Thinking docs should not imply a wire-level parameter implementation here.

## Related Code Files

- Modify: `internal/http/provider_models_catalog.go`
- Modify: `docs/02-providers.md`
- Modify: `docs/12-extended-thinking.md`
- No UI changes: web and desktop use the existing provider models endpoint.

## Implementation Steps

1. Add `qwen3.7-plus` near the current Qwen Plus entries in `bailianModels()`.
2. Add concise inline catalog grouping that records Bailian capabilities for Qwen Plus models.
3. Update `docs/02-providers.md` Bailian section with the new model and capability row.
4. Update `docs/12-extended-thinking.md` to clarify Bailian advertises Deep Thinking for `qwen3.7-plus`, but GoClaw does not inject DashScope `enable_thinking` controls for Bailian in this change.

## Success Criteria

- [ ] Catalog contains `qwen3.7-plus`.
- [ ] Docs mention Text Generation, Deep Thinking, Visual Understanding.
- [ ] No new provider request-body controls are introduced.

## Risk Assessment

Risk: adding reasoning metadata could make Bailian receive unsupported OpenAI `reasoning_effort`.
Mitigation: leave `LookupReasoningCapability()` unchanged and document the distinction.

## Security Considerations

No new secrets, auth paths, or tenant writes.
