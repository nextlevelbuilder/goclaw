---
phase: 4
title: Web UI credential management
status: completed
effort: ''
---

# Phase 4: Web UI credential management

## Context Links

- CLI credentials table action: `ui/web/src/pages/cli-credentials/cli-credentials-table.tsx:70`
- Current user credential dialog: `ui/web/src/pages/cli-credentials/cli-user-credentials-dialog.tsx:70`
- Current git typed fields: `ui/web/src/pages/cli-credentials/cli-credential-git-fields.tsx`
- Current hooks: `ui/web/src/pages/cli-credentials/hooks/use-cli-credentials.ts`
- Current i18n namespace: `ui/web/src/i18n/locales/en/cli-credentials.json`

## Overview

Make agent credentials the primary UI path for git PAT and SSH setup. Keep user credentials available but visually demote them to advanced personal overrides.

Priority: P1.

Status: pending.

## Key Insights

- Current UI exposes git typed fields inside `User Credentials`, which requires operators to know a stable `user_id`.
- Issue #117 asks for obvious `GH_PAT` or SSH fields. The primary action should therefore be per-agent credential setup from the git template row.
- Agent access becomes the practical permission boundary. UI must say this plainly without leaking secrets.

## Requirements

- Add Agent Credentials action/button in the CLI credentials table.
- For git adapter rows, default the credential form to PAT with `host_scope = github.com` placeholder.
- Support SSH key as second option.
- Show effective credential source where useful: user override, context, agent, binary.
- Move User Credentials into advanced/personal override wording.
- Keep mobile-safe dialog behavior.
- Add en/vi/zh i18n keys.

## Architecture

Preferred UI structure:

- Main table actions:
  - Grants
  - Agent Credentials
  - Advanced: User Credentials
  - Edit
  - Delete
- New dialog:
  - agent picker
  - credential type selector
  - host scope input
  - PAT token field or SSH private key textarea
  - masked secret state on edit
  - help text explaining agent access implies credential use
- Use existing `CliCredentialGitFields` by extracting labels/state shape into reusable props if needed.

## Related Code Files

- Add `ui/web/src/pages/cli-credentials/cli-agent-credentials-dialog.tsx`.
- Reuse or refactor `cli-credential-git-fields.tsx`.
- Extend `ui/web/src/pages/cli-credentials/hooks/use-cli-credentials.ts`.
- Update `cli-credentials-table.tsx` and panel state.
- Update all locale files under `ui/web/src/i18n/locales/{en,vi,zh}/cli-credentials.json`.
- Add/update tests in `ui/web/src/pages/cli-credentials/__tests__/`.

## Implementation Steps

1. Add API hook methods:
   - `listAgentCredentials(binaryId)`
   - `getAgentCredential(binaryId, agentId)`
   - `setAgentCredential(binaryId, agentId, payload)`
   - `deleteAgentCredential(binaryId, agentId)`
2. Add `CliAgentCredentialsDialog`.
3. Reuse typed git fields and env vars section. Avoid copy-pasting validation logic unless component boundaries require it.
4. Update table/panel to open Agent Credentials as the main credential action.
5. Rename current User Credentials copy to "Advanced user overrides" or equivalent in all locale files.
6. Add a warning/info line: users with access to this agent can cause it to use this credential.
7. Add UI tests:
   - git row shows Agent Credentials action
   - PAT form posts to `/agent-credentials/{agentId}`
   - edit state shows masked secret and does not submit empty replacement
   - User Credentials remains reachable as advanced override
8. Run `pnpm` tests/build for `ui/web`.

## Todo List

- [ ] Agent credential dialog implemented.
- [ ] Hooks added for all new endpoints.
- [ ] Table/panel actions updated.
- [ ] i18n updated in en/vi/zh.
- [ ] UI tests pass.

## Success Criteria

- [ ] Operator can configure `GH_PAT` for `github.com` without typing a channel user ID.
- [ ] Operator can configure SSH private key for a host-scoped git remote.
- [ ] User Credentials path remains available but no longer looks like the default git setup.
- [ ] UI makes the agent-access security boundary visible.

## Risk Assessment

- Risk: table action area becomes crowded. Mitigation: use icon buttons/tooltips or a compact menu if needed.
- Risk: duplicated form state between user and agent dialogs. Mitigation: extract only the shared typed git fields, not the whole dialog.
- Risk: mobile overflow in credential dialog. Mitigation: keep `max-h` scroll region and mobile-safe input sizes.

## Security Considerations

- Never render raw stored token/key after save.
- Clear plaintext form state on close/unmount.
- Do not store plaintext in Zustand or route state.

## Next Steps

- Phase 5 validates runtime behavior against the new source model.
