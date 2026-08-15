# Discord Qualified Group Titles

**Date**: 2026-07-10
**Component**: Discord channel metadata and delivery routing
**Status**: Resolved

## Context

Discord group names can be ambiguous when a thread and its parent need to be distinguished. Passive Memory continues to retain the raw `group_title` and `parent_group_title` independently; this change adds only a qualified display title, `thread / parent`, as presentation metadata.

## What Happened

The qualified title is persisted and now follows the group through contacts, delivery targets, current context, cron jobs, heartbeats, and synthetic re-ingress. Stable Discord identifiers were deliberately left unchanged, so routing and stored relationships retain their existing identity contract.

The backfill gap was traced to archived or inactive groups falling outside the prior lookup window. Backfill now uses stable pagination beyond 100 results, so qualifying stored groups does not depend on their activity or sort position.

## Decision

The inbound hot path must not perform a REST lookup merely to decorate a title. An attempted REST fallback there was rejected and corrected: only synthetic re-ingress resolves live Discord state, where the extra lookup is appropriate.

Stored IDs that cannot be read now produce an explicit refresh failure rather than silently retaining stale or incomplete presentation metadata. Together with stable pagination, this makes maintenance behavior observable and avoids a partial, misleading backfill.

## Verification

- `go test ./...`
- PostgreSQL and SQLite builds
- `go vet ./...`
- Web lint and production build
- Review P1 finding fixed

## Reflection

Keeping raw memory fields separate from display metadata prevents a UI convenience from silently becoming an identity or memory-schema change. Qualifying at the presentation boundary preserves useful context without adding latency to ordinary inbound messages.
