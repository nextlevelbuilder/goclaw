---
title: Add qwen3.7-plus to Bailian Coding provider
description: ''
status: pending
priority: P2
issue: 169
branch: codex/issue-169-qwen37-bailian-model
tags: []
blockedBy: []
blocks: []
created: '2026-06-12T10:09:09.353Z'
createdBy: 'ck:plan'
source: skill
---

# Add qwen3.7-plus to Bailian Coding provider

## Overview

Add `qwen3.7-plus` to GoClaw's hardcoded Bailian Coding model catalog so the shared `/v1/providers/{id}/models` endpoint exposes it to both web and desktop model pickers. Keep runtime request behavior backward-compatible: Bailian remains an OpenAI-compatible provider, and no GPT/Codex `reasoning_effort` metadata is added for this Qwen model.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Catalog and capability docs](./phase-01-catalog-and-capability-docs.md) | Completed |
| 2 | [Tests and verification](./phase-02-tests-and-verification.md) | Completed |
| 3 | [Ship beta PR](./phase-03-ship-beta-pr.md) | Pending |

## Dependencies

- Source issue: <https://github.com/digitopvn/goclaw/issues/169>
- Verified code paths:
  - `internal/http/provider_models_catalog.go`: `bailianModels()` hardcoded catalog.
  - `internal/http/provider_models.go`: Bailian dispatch uses `bailianModels()` then `withReasoningCapabilities()`.
  - `internal/http/providers.go`: Bailian runtime registration uses `providers.NewOpenAIProvider(...)`.
  - `internal/providers/openai_request.go`: `reasoning_effort` is gated by `LookupReasoningCapability()`, so do not add `qwen3.7-plus` there.

## Acceptance Criteria

- [x] `qwen3.7-plus` appears in the Bailian Coding model catalog.
- [x] Display name is `Qwen 3.7 Plus`.
- [x] Capabilities are documented as Text Generation, Deep Thinking, Visual Understanding.
- [x] Web/Desktop provider model pickers inherit the model through the existing `/v1/providers/{id}/models` API.
- [x] Existing Bailian provider registration remains backward-compatible.
- [x] Tests cover the Bailian model list.

## Unresolved Questions

- Does Bailian expose explicit request parameters for Deep Thinking on `qwen3.7-plus`? Out of scope for this catalog update unless verified separately.
