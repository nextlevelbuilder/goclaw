---
phase: 1
title: "Characterization Tests"
status: complete
priority: P1
effort: "2h"
dependencies: []
---

# Phase 1: Characterization Tests

## Overview

Write failing tests that lock the expected Discord thread behavior before implementation. Tests must prove current behavior does not backfill thread history from Discord REST and does not expose prior attachments to the inbound media pipeline when runtime has no pending history.

## Requirements

- Functional: when a mentioned Discord thread message arrives, prior thread messages before that message are fetched and represented as context.
- Functional: prior thread attachments are downloaded and included in published inbound media.
- Non-functional: tests run without live Discord by using `httptest` around discordgo REST calls or a narrow test seam.

## Architecture

Target the handler boundary. Build a `discordgo.Session` pointed at a local test server, return `/channels/{threadID}/messages?before={currentID}&limit=N`, and inspect the published `bus.InboundMessage`.

## Related Code Files

- Modify: `internal/channels/discord/handler_test.go`
- Modify or create focused test file: `internal/channels/discord/thread-history-backfill_test.go`
- Read: `internal/channels/discord/handler.go`
- Read: `internal/channels/discord/media.go`
- Read: `internal/bus/types.go`

## Implementation Steps

1. Add a test channel fixture that can publish inbound messages to a real `bus.MessageBus` and use a local discordgo REST endpoint.
2. Add a failing test for a public/private Discord thread: current message mentions bot; REST history returns one earlier user message; published content includes the earlier message before current message.
3. Add a failing test where earlier history message has image and document attachments; test server serves attachment bytes; published inbound `Media` includes those files with useful filenames/MIME types.
4. Add a failing test for missing/empty history response: handler still publishes current message and logs/continues without panic.
5. Keep tests deterministic: no real network, no live Discord token, no sleeps.

## Success Criteria

- [x] Tests fail on current code for missing thread backfill.
- [x] Tests assert order: historical messages oldest-to-newest, then current message.
- [x] Tests assert media count and filenames/MIME where available.
- [x] No live credentials or real Discord calls.

## Risk Assessment

Risk: handler is large and hard to test. Mitigation: keep the test seam focused on Discord REST and bus output; do not refactor unrelated handler behavior in this phase.
