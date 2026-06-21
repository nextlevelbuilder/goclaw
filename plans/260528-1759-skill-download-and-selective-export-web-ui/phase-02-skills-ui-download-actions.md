---
phase: 2
title: "Skills UI Download Actions"
status: complete
priority: P2
effort: "0.75d"
dependencies: [1]
---

# Phase 2: Skills UI Download Actions

## Context Links

- Skills page: `ui/web/src/pages/skills/skills-page.tsx`
- Skills hook: `ui/web/src/pages/skills/hooks/use-skills.ts`
- Detail dialog: `ui/web/src/pages/skills/skill-detail-dialog.tsx`
- Bulk toolbar: `ui/web/src/pages/skills/skill-bulk-actions-toolbar.tsx`
- Row actions: `ui/web/src/pages/skills/skill-table-row.tsx`
- Existing export hook pattern: `ui/web/src/pages/import-export/hooks/use-capabilities-export.ts`
- i18n: `ui/web/src/i18n/locales/{en,vi,zh}/skills.json`

## Overview

Expose admin-only download actions where users already manage skills: one action in skill detail and one bulk action for selected rows.

## Requirements

- Functional: admin can download current skill from detail dialog.
- Functional: admin can select multiple skills and download one archive.
- Functional: selected system/core skills are eligible.
- Functional: UI supports archive format choice.
- Functional: empty selection is disabled and surfaced clearly.
- Non-functional: use existing `HttpClient.downloadBlob` and auth headers.
- Non-functional: no client-side ZIP assembly.

## Architecture

Use backend as source of truth. UI constructs download URLs only:

```text
SkillDetailDialog
  -> useSkills().downloadSkills({ ids: [skill.id], format })
  -> /v1/skills/export?id=<id>&format=<format>

SkillBulkActionsToolbar
  -> useSkills().downloadSkills({ ids: selectedIds, format })
  -> /v1/skills/export?ids=<comma-list>&format=<format>
```

Format control:
- Keep default `zip` in the Skills page because browser users expect ZIP.
- Offer `zip`, `tar.gz`, `tgz` in a compact select/menu.
- Import/export page can continue defaulting to tar.gz for compatibility.

## Related Code Files

- Modify: `ui/web/src/pages/skills/hooks/use-skills.ts`
- Modify: `ui/web/src/pages/skills/skills-page.tsx`
- Modify: `ui/web/src/pages/skills/skill-detail-dialog.tsx`
- Modify: `ui/web/src/pages/skills/skill-bulk-actions-toolbar.tsx`
- Modify: `ui/web/src/i18n/locales/en/skills.json`
- Modify: `ui/web/src/i18n/locales/vi/skills.json`
- Modify: `ui/web/src/i18n/locales/zh/skills.json`
- Create or extend tests near `ui/web/src/pages/skills/lib/` if logic is extracted.

## Tests Before

1. Add UI logic tests for:
   - export URL params from one ID and many IDs.
   - filename extension from selected format.
   - no selected IDs -> disabled bulk download.
2. Add component tests only if existing test setup makes it low-friction; otherwise keep logic in pure helpers.

## Refactor

1. Add a small helper:
   - `buildSkillExportParams(ids, format)`
   - `skillExportDownloadName(skills, format)`
2. Add `downloadSkills(ids, format)` to `useSkills`.
3. Add local `exportFormat` state in `SkillsPage` or small child component.
4. Wire bulk toolbar download button.
5. Wire detail dialog download button near Copy Link/version controls.
6. Add i18n keys in all three locale files before rendering strings.

## Tests After

1. Verify pure helper tests pass.
2. Verify TypeScript build catches prop wiring:
   - `pnpm -C ui/web build`
3. Manual UI check if dev server is available:
   - custom tab selected skills -> download enabled.
   - core tab selected skills -> download enabled.
   - no selected skills -> no bulk toolbar.

## Implementation Steps

1. Write failing helper tests for URL/query and filename behavior.
2. Add helper module only if it prevents bloating `skills-page.tsx`; use kebab-case filename.
3. Add `downloadSkills` in `useSkills` using `http.downloadBlob`.
4. Pass download callbacks into `SkillBulkActionsToolbar` and `SkillDetailDialog`.
5. Add format selector using existing UI select/menu patterns.
6. Add loading state so repeated clicks are disabled.
7. Add success/error toasts.
8. Update all `skills.json` locale files.

## Todo List

- [x] URL/filename helper tests first.
- [x] `useSkills` exposes `downloadSkills`.
- [x] Detail dialog has single-skill Download.
- [x] Bulk toolbar has Download selected.
- [x] Core/system selected rows are not filtered out.
- [x] EN/VI/ZH strings added.
- [x] Web build passes.

## Success Criteria

- [x] Admin can download one skill from detail.
- [x] Admin can download selected custom and system skills from list.
- [x] UI can choose `zip`, `tar.gz`, or `tgz`.
- [x] Empty selection and loading states are correct.
- [x] UI does not assemble archives client-side.
- [x] `pnpm -C ui/web test -- skills` and `pnpm -C ui/web build` pass.

## Risk Assessment

- Risk: adding too much state to `skills-page.tsx`.
  Mitigation: extract helpers and keep UI state minimal.
- Risk: "Download" icon conflicts with dependency install icon.
  Mitigation: labels and tooltip text clarify skill export vs dependency install.
- Risk: admin-only feature visible to non-admin due route assumptions.
  Mitigation: Skills page route/admin context should be checked; backend remains source of enforcement.

## Security Considerations

- UI visibility is convenience only; backend admin gate is authoritative.
- Do not expose raw archive token or filesystem path in UI state.

## Next Steps

Phase 3 validates backend and UI together, then comments the issue with plan path and implementation summary.
