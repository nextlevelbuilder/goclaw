# Meow Telegram Autopilot — P3 publish path + P4 draft ingest

**Date:** 2026-06-14 · **Branch:** `feat/meow-telegram-autopilot` · **Plan:** `plans/260612-2129-meow-telegram-channel-autopilot/`

Continues the P2 journal. This arc built the **deterministic, owner-gated publish path** (P3) and the **draft ingest pipeline** (P4 steps 1–3), in small reviewed commits. Each code seam was unit-tested; every DB-bound claim was proven against a real `pgvector/pgvector:pg18` run on the VPS through an SSH tunnel (no local Docker in-session).

## P3 — publish capability (code-complete, 10 commits)
Pipeline, all deterministic (no LLM on the post path): **validate → claim → caption → send → persist**.
- `meow.ValidateButtonURL` (exact registered-URL allowlist + host layer), `ValidateImagePath` (symlink-resolved containment), `BuildCaption`/`SplitForPhoto` (KO/EN escape + UTF-16-unit split).
- `MeowStore.ClaimPostForPublish` — atomic `FOR UPDATE SKIP LOCKED` + NOT-EXISTS guard ⇒ exactly-once; `meow.Publisher.PublishDue` orchestrates and leaves the row `publishing` for manual review on any post-claim failure (never blind-retry).
- telegram `SendChannelPost` (photo + inline buttons + returns message id) → `channels.ChannelPoster` seam → `Manager.PublishChannelPost` (direct, not via `bus.OutboundMessage`).
- `publish_channel_post` tool wired to the real Manager at gateway startup; predefined **Meow agent** seeded with a tool allowlist of only real registered tools.
- **Closed-by-default `OwnerGate`** + verification state in system config; `/meow verify|post` command (owner-gated, round-trip verify).

## P4 — draft pipeline (steps 1–3, 4 commits)
- **Image transport, proven on the VPS first** (riskiest assumption): the container runs `CapDrop=[ALL]` with no userns remap, so in-container root can't write the `1000`-owned data volume — transport goes via the **host volume path**; a uid-1000 process then reads it at `/app/data/meow-assets/...`. Decision doc: `docs/meow-image-transport.md`.
- **Draft bundle schema** (`meow.DraftBundle`, strict parse) + structural validation; `docs/meow-draft-bundle.md`.
- **Ingest** (`meow.IngestDraft`): validate → resolve channel → exact button allowlist → image existence + **WebP signature** → copy into `meow-assets` → upsert draft. Any content problem **holds** (no DB row). `UpsertDraftPost` is idempotent (re-ingest = one updated row, PG-proven). `image_path` persists only the container path, never a Mac/source path.

## What the review loop caught (the valuable part)
Across this arc, external static review caught defects I'd have shipped:
- **Tenant isolation on child *writes*** — reads were scoped but `CreatePost`/`UpsertMetric`/`CreateOrder` trusted a caller tenant against a parent owned by another tenant. Fixed with composite `(child, tenant)` FKs + `INSERT…SELECT WHERE EXISTS(parent tenant)`.
- **Startup seed erasing runtime state** — `UpsertChannel` ON CONFLICT clobbered chat_id/enabled/smm; now preserves runtime fields.
- **Button allowlist too weak** — host-only let any `t.me/<bot>` through; now requires exact registered-URL membership.
- **Image type unchecked** — `.webp` name wasn't a guarantee; added RIFF/WEBP signature check before a draft can exist.
- Caption hard-split could cut an HTML entity; plan-label leakage in code/docs. All fixed.

## Testing posture
Local: `go build` (both tags) + `vet` + unit suites each commit. DB-bound: spun a throwaway `pgtest-meow` container on the VPS, tunneled `-L 5433`, ran the integration suite (9 `TestMeowStore_*` green incl. exactly-once claim + idempotent draft upsert), tore it down. Never touched production data.

## Carried forward
- **P4 step-4:** ingest command/tool wrapper; owner-gated `/meow queue|preview|edit|approve|skip`; live-edit (`EditMessageCaption`/`EditMessageReplyMarkup`); i18n (en/vi/zh).
- **Live post (step-5):** still gated on G1 (bot token/handle + admin probe), G2 (owner round-trip), and a real approved draft. Target: `@kingboardgamesofficial` (small).
- **Remote CI** not run (fork workflow-enable gate); VPS PG run is the accepted proof.

## Commit ledger (this arc)
P3: validators+caption → PublishDue+claim → SendChannelPost → Manager seam → tool+owner-gate → tool registration → agent scaffold → owner state+injection → /meow command. P4: G7 transport doc → bundle schema → ingest → WebP/doc-label fix.
