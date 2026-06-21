# Scout Report

## Findings

- Repo verified: `digitopvn/goclaw`, default branch `dev`.
- Source issue verified: #169 requests `qwen3.7-plus` for Bailian Coding.
- Catalog source: `internal/http/provider_models_catalog.go` -> `bailianModels()`.
- Endpoint dispatch: `internal/http/provider_models.go` uses `bailianModels()` for `provider_type == "bailian"`.
- UI propagation: web and desktop model pickers call `/v1/providers/{id}/models`; no hardcoded Bailian model list found in UI.
- Runtime boundary: `internal/http/providers.go` registers Bailian through `providers.NewOpenAIProvider(...)`.
- Wire guardrail: `internal/providers/openai_request.go` sends OpenAI `reasoning_effort` only when `LookupReasoningCapability()` returns metadata or model is GPT/o-series.

## Decision

Implement catalog entry, endpoint regression test, and docs. Do not add `qwen3.7-plus` to the GPT/Codex reasoning capability registry in this task.

## Unresolved Questions

- Does Bailian expose explicit thinking request parameters for `qwen3.7-plus`? Not needed for the catalog issue.
