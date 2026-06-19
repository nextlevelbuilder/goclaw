# Tester Report - issue #169 Bailian qwen3.7-plus catalog

## Scope
- Verified the Bailian Coding model catalog addition in `internal/http/provider_models_catalog.go`.
- Verified handler coverage in `internal/http/provider_models_test.go`.
- Checked docs updates in `docs/02-providers.md`, `docs/12-extended-thinking.md`, and `docs/project-changelog.md`.

## Results
- `PATH=/usr/local/go/bin:$PATH go test ./internal/http -run TestProvidersHandlerListProviderModelsBailianIncludesQwen37Plus -count=1`
  - PASS
- `git diff --check`
  - PASS

## Notes
- `GET /v1/providers/{id}/models` is the contract used by the web and desktop pickers; no UI hardcoded model list change was needed.
- Bailian registration remains on the existing OpenAI-compatible provider path with the default model still set to `qwen3.5-plus`.

## Coverage gap
- Catalog is covered by one focused regression only; no additional provider-level or UI-level test was added in this patch.
