## Outcome
Add `qwen3.7-plus` to the Bailian Coding provider model catalog so the shared provider model API exposes it to web and desktop model pickers.

## Implementation
- Branch: `codex/issue-169-qwen37-bailian-model`
- Plan: `plans/260612-1709-qwen37-bailian-model/plan.md`
- Mode: `beta`
- PR: pending

## Acceptance Criteria
- [ ] `qwen3.7-plus` appears in Bailian Coding model catalog
- [ ] Display name is `Qwen 3.7 Plus`
- [ ] Capabilities are documented/represented as Text Generation, Deep Thinking, Visual Understanding
- [ ] Web provider model picker can select `qwen3.7-plus` through `/v1/providers/{id}/models`
- [ ] Desktop provider model picker can select `qwen3.7-plus` through the same backend model API
- [ ] Existing Bailian provider registration remains backward-compatible
- [ ] Tests cover the Bailian model catalog
- [ ] Docs updated where provider model support is documented

## Pipeline State
- [x] Worktree and branch created
- [x] TDD plan created
- [x] Plan validated
- [x] Plan red-teamed
- [ ] Cook complete
- [ ] PR reviewed and fixed
- [ ] Merged and CI green (only when --ship)
