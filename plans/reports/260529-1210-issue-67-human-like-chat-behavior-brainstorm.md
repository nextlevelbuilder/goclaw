---
title: "Issue 67 Human-Like Chat Behavior Brainstorm"
date: "2026-05-29"
status: approved
issue: 67
branch: "codex/issue-67-human-like-chat-behavior"
source: "ck:brainstorm"
---

# Issue 67 Human-Like Chat Behavior Brainstorm

## Summary

Approved MVP: runtime config, quick acknowledgement, safe final multi-message splitting for channel delivery, dashboard/API preview.

Out of scope: archive/timeline storage, issue #76 renderer, per-agent overrides, Web UI ack delivery, streaming channel ack delivery.

## Codebase Findings

- Go 1.26 backend, React 19/Vite web UI, Wails desktop. Config uses JSON5 plus WS `config.patch`.
- Existing `block.reply` already emits intermediate assistant content during tool iterations from `internal/pipeline/think_stage.go`.
- Channel manager already resolves `gateway.block_reply` plus per-channel overrides in `internal/channels/runs.go`.
- Non-streaming channel delivery already uses `internal/channels/events.go` to publish `block.reply` as outbound messages.
- Existing channel chunking exists in `internal/channels/chunking.go` and platform adapters; MVP should reuse/extend this, not add per-platform duplicate splitters.
- Issue #76 plan owns durable run archive/timeline. This MVP must not add archive persistence or renderer work.

## Requirements

Expected output:
- Non-streaming channel deliveries can send a quick acknowledgement before longer/tool work.
- Final assistant content can be split into multiple safe outbound messages.
- Dashboard config and API/WS preview surface exist for global gateway and per-channel overrides.

Acceptance:
- Ack does not send for Web UI, streaming channel runs, silent replies, disabled config, or trivial runs.
- Splitter preserves fenced code blocks, block quotes, markdown tables, lists, structured JSON/YAML/XML, links, and short messages.
- Config resolution supports global gateway defaults and per-channel override only.
- Preview API returns deterministic ack/split decisions without dispatching messages.
- Tests cover splitter edge cases, ack gating, config resolution, final dedup with `block.reply`, and disabled/group-safe defaults.

Constraints:
- Do not implement per-agent override in this slice.
- Do not touch issue #76 timeline storage or archive renderer.
- Use existing config and channel patterns.
- Keep backward compatibility: default off or conservative.
- Add i18n for new UI strings in en/vi/zh.

## Options

| Option | Pros | Cons | Decision |
|---|---|---|---|
| Channel-layer policy only | Smallest change | Ack timing weak, too late for complexity gate | Reject |
| Run-context policy plus outbound helpers | Fits `RunContext`, clean delivery boundary, avoids #76 overlap | Needs careful dedup and tests | Choose |
| Agent pipeline behavior stage | Centralized near agent output | Over-engineered, mixes delivery behavior into reasoning runtime | Reject |

## Final Design

Use a channel delivery policy resolved at run registration time.

Config:
- `gateway.chat_behavior.enabled`
- `gateway.chat_behavior.quick_ack.enabled`
- `gateway.chat_behavior.quick_ack.min_delay_ms`
- `gateway.chat_behavior.quick_ack.templates`
- `gateway.chat_behavior.final_split.enabled`
- `gateway.chat_behavior.final_split.min_chars`
- `gateway.chat_behavior.final_split.max_messages`
- `gateway.chat_behavior.final_split.delay_ms`
- per-channel `chat_behavior` override with same shape, nil fields inherit global.

Runtime:
- Resolve global plus channel override when `RegisterRun` creates `RunContext`.
- Send ack from early run event handling only when non-streaming and enabled.
- Prefer conservative gate: tool-capable/tool-run signal, non-streaming, no prior block reply, no streaming.
- Split final content after sanitization and before outbound publish.
- Preserve `block.reply` behavior. If final equals last block reply, keep existing dedup semantics.

API/UI:
- Add preview handler for ack and split output. No side effects.
- Add global dashboard controls under Behavior.
- Add per-channel override controls in channel schema.

## Risks

- Spam risk in group chats. Mitigate with default off, non-streaming only, and max message caps.
- Splitter can break markdown. Mitigate with tests-first contract and conservative "do not split" fallback.
- Ack can race with final response. Mitigate by min delay and skip when run completes quickly.
- Config shape drift. Mitigate with JSON-compatible structs and nil inheritance.

## Next Steps

1. Create TDD plan.
2. Validate and red-team plan.
3. Commit and push planning artifacts.
4. Comment GitHub issue #67 and label `ready to implement`.
5. Implement via TDD plan.

## Unresolved Questions

None.
