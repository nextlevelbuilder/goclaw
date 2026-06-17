---
phase: 6
title: Docs validation and handoff
status: completed
effort: ''
---

# Phase 6: Docs validation and handoff

## Context Links

- Git guide: `docs/git-credential-adapter.md`
- HTTP API docs: `docs/18-http-api.md`
- API auth docs: `docs/20-api-keys-auth.md`
- Security docs: `docs/09-security.md`
- Store model docs: `docs/06-store-data-model.md`
- Project changelog: `docs/project-changelog.md`

## Overview

Update user-facing and developer docs, run validation, and leave a clean handoff for implementation review.

Priority: P1.

Status: pending.

## Key Insights

- The current git guide says to open User Credentials. That will become the advanced path.
- API docs must include the new endpoints because user specifically asked for endpoint support.
- Docs must state the trust model: agent access implies ability to cause that agent to use its configured git credential.

## Requirements

- Update docs for agent credential default.
- Keep user credential override documented.
- Add HTTP API endpoint table and examples.
- Update security/data-model docs.
- Update changelog.
- Run code, test, and build validation appropriate to touched files.

## Architecture

Documentation model:

- Quick start: create git CLI credential, grant/use agent, add agent credential.
- PAT path: fine-grained PAT preferred where possible, `host_scope = github.com`.
- SSH path: unencrypted private key only, public key added to git host by operator.
- Advanced path: per-user credential overrides for personal credentials.
- Security model: any principal with access to run the agent can trigger the credential.

## Related Code Files

- `docs/git-credential-adapter.md`
- `docs/18-http-api.md`
- `docs/20-api-keys-auth.md`
- `docs/09-security.md`
- `docs/06-store-data-model.md`
- `docs/project-changelog.md`

## Implementation Steps

1. Rewrite git guide "Adding a credential" around Agent Credentials first.
2. Add "Advanced user overrides" section.
3. Add endpoint documentation for all agent credential routes.
4. Add request/response examples for PAT and SSH.
5. Update security docs with trust boundary and secret masking.
6. Update data model docs with `secure_cli_agent_credentials`.
7. Update changelog with issue #117 entry.
8. Run validation:
   - `go test ./internal/store/... ./internal/http/... ./internal/tools/...`
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
   - `cd ui/web && pnpm test -- --run`
   - `cd ui/web && pnpm build`
9. If full integration tests are skipped due local database requirements, state that explicitly in final handoff.

## Todo List

- [ ] Git guide updated.
- [ ] HTTP/API auth docs updated.
- [ ] Security and data-model docs updated.
- [ ] Changelog updated.
- [ ] Validation commands recorded with pass/fail status.

## Success Criteria

- [ ] A new operator can find where to enter `GH_PAT` or SSH key without knowing channel user IDs.
- [ ] API consumers can manage agent credentials without reading code.
- [ ] Security docs describe agent access as the permission boundary.
- [ ] Build/test output supports merge readiness.

## Risk Assessment

- Risk: docs overpromise support for GitHub App or passphrase SSH. Mitigation: keep out-of-scope section explicit.
- Risk: endpoint examples drift from implementation. Mitigation: generate examples from handler tests where practical.

## Security Considerations

- Do not include real tokens, keys, or screenshots with secrets.
- Use placeholder values only.
- State least-privilege recommendation: fine-grained PAT or deploy key per host/repo where possible.

## Next Steps

- After implementation and validation, open PR referencing issue #117 and this plan.
