# Meow Sheets approval bridge — VPS rollout runbook

Two-stage rollout for the Google Sheets approval-bridge sync worker. **Stage 1 is
sync-only (no Telegram posting); only move to Stage 2 after one approved cycle is
observed.** Deploy normally from a merged build — do **not** rebuild prod from the
feature branch.

## Prerequisites
- Branch merged and the standard image deployed/restarted normally.
- Service-account key on the VPS, readable by the app (uid 1000). Recommended: place
  it in the data volume and point at it by path.
  ```
  host:      /var/lib/docker/volumes/goclaw_goclaw-data/_data/secrets/meow-sheets-sa.json
  container: /app/data/secrets/meow-sheets-sa.json
  owner/mode: chown 1000:1000, chmod 600
  ```
- The "Meow Publishing Queue" spreadsheet is shared (Editor) with
  `meow-sheets@ton-corp.iam.gserviceaccount.com` (id `15OsWmsKDAua2lq3OyoYeNdUo3BjNVa1XT2u1ZSMGsws`).
- For any row to publish later, its image is already transported to the inbox
  (`/app/data/meow-inbox/<brand_key>/<date>.webp`, 1000:1000) — manual scp, unchanged.

## Config keys
Secrets/settings via env (never config.json). Pass through `docker-compose.override.yml`
`environment:` (compose `.env` vars are not auto-injected into the container).

| Env | Meaning |
|-----|---------|
| `GOCLAW_MEOW_SHEETS_ENABLED` | master on/off for the worker (two-way: false/off/0 force-disables) |
| `GOCLAW_MEOW_SHEETS_PUBLISH` | live-publish master switch (two-way; default off) |
| `GOCLAW_MEOW_SHEETS_SPREADSHEET_ID` | the queue spreadsheet id |
| `GOCLAW_MEOW_SHEETS_CREDENTIALS_FILE` | path to SA JSON in the container (or `..._CREDENTIALS_JSON` inline) |
| `GOCLAW_MEOW_SHEETS_INTERVAL` | poll interval, e.g. `5m` (default 5m, floored 1m) |

## Stage 1 — sync-only (approve into DB; NO posting)
1. Set env: `GOCLAW_MEOW_SHEETS_ENABLED=true`, `GOCLAW_MEOW_SHEETS_SPREADSHEET_ID=…`,
   `GOCLAW_MEOW_SHEETS_CREDENTIALS_FILE=/app/data/secrets/meow-sheets-sa.json`.
   Leave `GOCLAW_MEOW_SHEETS_PUBLISH` unset/false.
2. Recreate the container (compose up). Confirm the log line:
   `meow sheets sync enabled  spreadsheet=…  interval=5m  publish=false`.
3. On one real row (image already in the inbox), tick **manager_approved**.
4. Within one interval, verify: sheet row `status` → `approved`, and the DB has one
   approved post (`/meow queue <@handle>` or `/meow preview <@handle> <date>`).
   No Telegram post happens. Re-runs are idempotent (no duplicate posts).

## Stage 2 — enable publish (after Stage 1 is observed clean)
1. Set `GOCLAW_MEOW_SHEETS_PUBLISH=true`; recreate the container. Log now shows
   `publish=true`.
2. On the approved row, tick **publish_now**. Within one interval the worker runs the
   deterministic, exactly-once publish; verify the sheet row → `status=published` with
   `tg_message_id` + `tg_link`, and the post is live in the channel.
3. Exactly-once holds: re-runs never double-post. If a write-back is ever dropped after
   a successful publish, the next pass restores `status`/`tg_message_id`/`tg_link` from
   the DB.

## Kill switches
- Stop publishing only: `GOCLAW_MEOW_SHEETS_PUBLISH=false` (worker stays sync-only).
- Stop the worker entirely: `GOCLAW_MEOW_SHEETS_ENABLED=false`.
- Both are two-way env overrides — they force-disable even if config.json enables them.
Recreate the container after changing env.

## Notes
- `manager_approved` = approve into DB only; `publish_now` = the live-post trigger.
- Publishing requires BOTH `meow_sheets.publish` on AND a wired Telegram channel manager.
- The `/meow` owner commands remain the admin fallback and are unaffected.
