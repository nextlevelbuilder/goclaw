---
phase: 3
title: "Sidecar Delivery Message Generator"
status: completed
priority: P1
effort: "1d"
dependencies: [2]
---

# Phase 3: Sidecar Delivery Message Generator

## Overview

Implement a sidecar LLM generator for Quick Acknowledgement and Intermediate
Replies. It must be cheap, bounded, language-aware, and completely separate
from the main session context.

## Requirements

- Functional: generated delivery text can use configured provider/model,
  falling back to agent provider/model when unset.
- Functional: output follows the user's language and is natural, not tool-name
  based.
- Functional: timeout/error falls back to configured template.
- Non-functional: max output tokens, timeout, prompt size, and final chars are
  bounded.
- Non-functional: no tools, no session history, no main RunRequest.

## Architecture

Use an injected generator interface in channel runtime:

```go
type DeliveryMessageGenerator interface {
  GenerateDeliveryMessage(ctx context.Context, req DeliveryMessageRequest) (string, error)
}
```

The implementation can live outside provider-agnostic channel logic and call
`providers.Provider.Chat` directly with a tiny prompt, similar to
`agent.ClassifyIntentWithUsageCaps`. The request should include only: user
message preview, locale, peer kind, channel type, agent display name, and max
length, and purpose (`quick_ack` or `intermediate_progress`). For intermediate
progress, include only bounded tool-phase metadata such as tool category/count
or elapsed time; do not include raw tool arguments or tool output. Do not include
session history, tool schemas, system prompt, or memory.

Provider resolution must be explicit:
- Add a small provider resolver dependency to the consumer/channel seam. Current
  `ConsumerDeps` has no provider registry, while `providers.Registry.Get(ctx,
  name)` already supports tenant-aware lookup.
- Resolve sidecar provider/model once at run registration:
  configured provider/model > agent provider/model.
- Store the resolved provider handle/model or a tiny generator closure on the
  run snapshot; channel event handling should not perform mutable config lookup.

## Related Code Files

- Create: `internal/channels/delivery_message_generator.go` or
  `internal/agent/delivery_message_generator.go`
- Modify: `internal/channels/events.go`
- Modify: `internal/channels/manager.go`
- Modify: `cmd/gateway_consumer_normal.go`
- Modify: `cmd/gateway_consumer_deps.go`
- Modify: `cmd/gateway_consumer.go`
- Modify: `cmd/gateway_lifecycle.go`
- Modify: `internal/agent/intent_classify.go` only if sharing tiny-call helper
  code is cleaner than duplicating request boilerplate.
- Modify: tracing/usage caps integration only if existing provider calls require
  accounting.

## Implementation Steps

1. Add a fake-generator test seam first; do not import provider registry into
   tests that only need channel behavior.
2. Implement sidecar prompts with strict output contracts:
   one short acknowledgement/progress update, same language as user, no raw tool
   names, no promises, no markdown table, no more than configured chars.
3. Thread provider registry into `ConsumerDeps` from `gatewayRuntime`, then
   resolve sidecar provider/model at run registration from delivery config:
   explicit provider/model > agent provider/model.
4. Call generator from `scheduleQuickAck` after `min_delay_ms`; guard with the
   same cancellation and `blockReplySent` checks as current template ack.
5. On error, timeout, empty output, or unsafe output, send the first configured
   template when templates are present.
6. For tool-call progress, generate sidecar text from the delivery request and
   publish it as delivery-only content. Do not use `resp.Content` from
   `ThinkStage` as the sidecar message because that content is appended to main
   run messages for tool-call continuity.
7. Record logs/tracing metadata as delivery purpose, but do not create a chat
   session message.
8. Keep legacy `llm_generated` behavior as an alias or fallback only if needed
   for existing configs; the UI should prefer `sidecar_generated`.

## Success Criteria

- [x] Sidecar generated ack sends before the main run finishes when delay
      elapses.
- [x] Sidecar intermediate progress sends on bounded tool phases.
- [x] Sidecar delivery uses configured provider/model in tests.
- [x] Sidecar provider resolution uses tenant-aware registry lookup when a
      provider is configured, and uses agent provider/model when unset.
- [x] Sidecar failure falls back or suppresses without failing the user request.
- [x] No sidecar prompt contains session history or tool schemas.
- [x] No sidecar prompt contains raw tool arguments or tool output.
- [x] Generated text is sanitized and max-length bounded before outbound send.

## Risk Assessment

Risk: sidecar ack increases cost and provider pressure. Mitigate with disabled
default behavior, low token cap, short timeout, and template fallback.
