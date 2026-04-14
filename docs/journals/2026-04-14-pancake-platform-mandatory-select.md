# Pancake Platform: Mandatory Dropdown + Form Validation Gap Fix

**Date**: 2026-04-14 09:00
**Severity**: Medium
**Component**: `ui/web/src/pages/channels/`, `internal/channels/pancake/`
**Status**: Resolved

## What Happened

Pancake channel creation let operators leave the `platform` field blank or type a freeform string. Auto-detection from webhook payloads was the supposed fallback — but auto-detection is best-effort and silent-wrong when it fires on an ambiguous payload. An operator could create a Pancake channel, send messages through it for days, and only discover the platform was misidentified when behavior diverged between Facebook and Instagram routing.

Separately, the channel form had no pre-submit validation on `required` config fields. The schema marked fields as `required: true` but the form dialog never checked — it shipped empty strings to the backend and let the backend fail.

Both were fixed: platform field is now a required dropdown with 11 explicit options, and the form dialog enforces `required` config fields on submit (create path only, to avoid breaking legacy channels with no platform stored in DB).

## The Brutal Truth

The "auto-detect as fallback" design was wishful thinking. Auto-detection works when every webhook payload has a reliable, consistent platform discriminator. It doesn't work when platform discrimination is inferred from message shape rather than an explicit field. Shipping a form with an optional platform and crossing fingers on runtime detection was a correctness gamble that never needed to be made — the user knows which platform they're configuring at creation time.

The `required` validation gap is more embarrassing. The schema system had a `required` property on field descriptors, but the form dialog never read it before submitting. The property was there for documentation purposes only. This is the worst kind of dead code: it looks like it works, reviewers assume it works, and it silently fails to enforce anything.

## Technical Details

Schema change in `channel-schemas.ts`:

```ts
// Before
{ key: "platform", type: "text", required: false, hint: "Auto-detected if empty" }

// After
{ key: "platform", type: "select", required: true, defaultValue: "",
  options: [
    { value: "facebook", label: "Facebook" },
    { value: "instagram", label: "Instagram" },
    // ... 9 more
  ] }
```

Validation block added in `channel-instance-form-dialog.tsx`, scoped to create-only:

```ts
if (!isEditing) {
  const missing = schema.configFields
    .filter(f => f.required && !cleanConfig[f.key])
    .map(f => f.key);
  if (missing.length > 0) {
    setErrors({ config: missing });
    return;
  }
}
```

Validated against `cleanConfig` (post empty-string strip), not `configValues` — catches both `undefined` and `""` from an unselected dropdown. Validating `configValues` would have missed the `""` case because `cleanConfig` strips empty strings before the check.

Backend: `pancake.go` auto-detect log demoted from `slog.Info` to `slog.Debug`. The previous level would have spammed logs for every legacy channel restart indefinitely.

Test run: 22 UI tests pass (`channel-schemas.test.ts` + pre-existing suite). Go pancake tests pass. PG and SQLite builds clean.

## What We Tried

No dead ends. TDD approach: wrote `channel-schemas.test.ts` with 5 failing assertions (Red), implemented the schema change, confirmed Green (22/22). The test-first pass caught a defaultValue omission before it would have caused a subtle unset-state bug in the dropdown.

## Root Cause Analysis

Two independent oversights:

1. Platform field: schema author assumed auto-detection was robust enough to make explicit selection optional. It isn't. Explicit always beats inferred for configuration that has deterministic user knowledge at creation time.

2. Required validation: the `required` property was added to the schema descriptor type without a corresponding enforcement pass in the form dialog. Classic schema-as-documentation antipattern — the property communicates intent but enforces nothing. No test existed to verify that submitting a blank required field was actually blocked.

## Lessons Learned

1. **If the user knows it at creation time, make them set it.** "Auto-detect as fallback" is only valid when the value genuinely cannot be known upfront. Platform is a deliberate operator choice, not a runtime observable.

2. **Schema properties that imply behavior must be enforced.** `required: true` in a field descriptor is a promise to the operator. If the form doesn't read it, the promise is a lie. Any time a new semantic property is added to a schema type, ask immediately: "where does this actually get enforced?"

3. **Validate `cleanConfig`, not `configValues`.** The form strips empty strings from config before submission. Validating before the strip means `""` from an unselected dropdown bypasses the required check. Validate the value in the same form it will be submitted.

4. **Edit-path must not block legacy data.** Existing Pancake channels in production have no `platform` stored — they relied entirely on auto-detection. Making `platform` required on edit would lock operators out of editing those channels until they backfill the field. Create-only enforcement + runtime auto-detect fallback is the right seam.

## Next Steps

- [ ] Confirm exact Pancake API keys for `threads`, `youtube`, `google`, `chat_plugin` against live Pancake docs or a real webhook payload. Current values are best-guess from naming convention — if any are wrong, routing will silently mismatch. Owner: whoever next handles a Pancake integration with those platforms.
- [ ] Backfill migration or UI prompt for existing Pancake channels with no `platform` stored — auto-detect will cover them at runtime but operators have no visibility into which platform was inferred. Low urgency, high debuggability value.
- [ ] Extend required-field validation to the edit path once legacy channels have been audited. Right now edit path has no guard. Owner: channels team, after confirming no production channel has an unset required field.
