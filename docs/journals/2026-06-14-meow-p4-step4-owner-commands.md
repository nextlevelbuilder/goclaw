# Meow Telegram Autopilot — P4 step-4 owner command surface

**Date:** 2026-06-14 · **Branch:** `feat/meow-telegram-autopilot` · **Plan:** `plans/260612-2129-meow-telegram-channel-autopilot/`

Continues the P3/P4 journal. This arc built the **owner-gated content command surface** — the human approval loop between draft ingest (P4 steps 1–3) and the deterministic publish path (P3) — in 5 small reviewed commits. Code seams unit-tested; the two new store methods proven against a throwaway `pgvector/pgvector:pg18` on the VPS over an SSH tunnel.

## Hardening first (1 commit)
- **Publish-boundary WebP re-check.** `Publisher.PublishDue` now calls `ValidateWebP` after `ValidateImagePath`, before `Sender.SendChannelPost` — defense-in-depth so a row crafted another way, or a file swapped on disk after ingest, can never put an unvetted/corrupt image on a live channel. New test proves a corrupt approved row is rejected with no send and no `published` write.
- Fallout the check correctly surfaced: the publish *tool* test used a non-WebP fixture → switched to a minimal `RIFF…WEBP` container. (Lesson: a behavior change at a shared boundary must re-run *consumer* package tests, not just its own.)

## Store: guarded review transitions (1 commit)
- `ApprovePost` (draft→approved, records `approved_by`+`approved_at`) and `SkipPost` (draft/approved→skipped). Both **status-guarded** in the `WHERE` clause: a wrong-state or cross-tenant target affects 0 rows → `ErrMeowPostNotFound` (a safe "nothing to do"), never retracts a publishing/published row. PG-proven (`TestMeowStore_ApproveAndSkip`: approve records state, re-approve no-op, skip from both source states, published untouched, cross-tenant rejected).

## Command surface + i18n (1 commit)
Owner-gated `/meow` subcommands, all closed-by-default (verified-owner gate checked **before** any DB/FS access): `queue` · `preview` · `edit` (draft caption, ko|en) · `approve` · `skip` · `ingest`. Wired via new `Channel.SetMeowOps(store, tenant, assetRoot, inboxRoot)` alongside the existing `SetMeowConfig`/`SetMeowPublisher` (same config-channel path P3 used). `handleMeowCommand` stays pure (returns a string) so the whole surface is unit-tested with a fake store + temp-dir ingest. 22 new `meow.*` i18n keys in en/vi/zh, resolved via `store.LocaleFromContext`.

## Inbox doc (1 commit)
`docs/meow-draft-inbox.md`: the fixed upload contract for `/meow ingest` — `/app/data/meow-inbox/<channel>/` (named volume, `1000:1000`), side-by-side `<date>.json`+`<date>.webp`, host-side `scp`+`cp` transport (caps-dropped container can't write the volume). Ingest joins the arg under the inbox root and `ValidateImagePath`-contains it (rejects `..`/symlink escape) before any read.

## Decisions (reviewer-confirmed)
- **No live-edit telego wrappers** (`EditMessageCaption`/`EditMessageReplyMarkup`) — `/meow edit` mutates pre-publish **drafts** via the DB, so they're off this path (YAGNI; only needed to edit already-sent messages).
- `/meow queue` requires an explicit `<@handle>` — avoids huge Telegram replies; revisit an all-channels view only after first live proof.
- `/meow edit` collapses whitespace runs (command dispatch is `strings.Fields`) — acceptable for quick admin fixes; canonical rich captions come from ingest bundles. A reply-based edit flow (not cleverer parsing) is the future answer if multiline matters.
- i18n vi/zh catalogs are in place but dormant: telegram doesn't yet thread a per-user locale into ctx, so replies render en (catalog fallback) until locale threading lands.

## Testing posture
Local each commit: `go build` (both tags) + `vet` + unit suites (`meow`, `tools`, `telegram`, `i18n`). DB-bound: spun a throwaway `pgtest-meow` on the VPS, tunneled `-L 5433`, ran the full Meow integration suite (10/10 green incl. the new approve/skip case), tore it down. No production data touched.

## Carried forward — live post (P4 step-5 / P5)
Live publish still gated on **G1** (bot token/handle + admin/post-rights probe), **G2** (owner chat_id round-trip), target channel confirmation (likely `@kingboardgamesofficial`), and one real approved draft on the server. P5 scheduler not started. Remote CI still not run (fork workflow-enable gate); VPS PG run is the accepted proof.

## Commit ledger (this arc)
WebP publish-boundary check → publish-tool fixture fix → guarded approve/skip → owner command surface + i18n → inbox doc.
