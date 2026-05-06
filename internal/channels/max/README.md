# Max Messenger Channel

Implementation of the goclaw `channels.Channel` interface for [Max](https://max.ru),
a Russian messaging platform.

User-facing documentation: [docs/05-channels-messaging.md § 12](../../../docs/05-channels-messaging.md#12-max-messenger).
This README is for goclaw contributors maintaining the channel.

## File map

| File | Purpose |
|------|---------|
| `doc.go`               | Package-level documentation |
| `factory.go`           | Channel factory (registered in `cmd/gateway.go`); config + creds JSON shapes |
| `types.go`             | Max API request/response types — wire format only |
| `client.go`            | HTTP client: GetMe, GetUpdates, SendMessage, EditMessage, DeleteMessage, PostAction, AnswerCallback, Subscribe/UnsubscribeWebhook |
| `max.go`               | Channel struct, lifecycle (Start/Stop), interface methods |
| `inbound.go`           | Long-polling loop, update dispatch, translator (Max Update → bus.InboundMessage) |
| `outbound.go`          | `Send` implementation with chunking, markdown, placeholder edit handoff |
| `format.go`            | `chunkText` and `safeUTF8Cut` helpers |
| `stream.go`            | `maxStream` (ChannelStream impl) + StreamingChannel methods |
| `webhook.go`           | WebhookChannel impl: `WebhookHandler()` and `serveWebhook` |
| `media_download.go`    | Inbound media: HTTP fetch with retry to `os.TempDir` |
| `media_upload.go`      | Outbound media: two-step Max upload flow + multipart push |
| `reactions.go`         | ReactionChannel impl: typing_on with per-chat refresher goroutine |
| `auth.go`              | `VerifyContactHash` for request_contact button auth |

Tests mirror the production files: `*_test.go` per module. Real PoC captures live in `testdata/*.json`.

## Architecture decisions

### Minimal upstream fork

This channel was added with **5 lines** of upstream change (`TypeMax` constant + factory registration in `cmd/gateway.go`); all logic lives in this package. Rationale: keeps merge surface small for syncing with `nextlevelbuilder/goclaw` upstream.

### Custom HTTP client (no SDK)

We deliberately did **not** wrap a third-party Max SDK. At the time this channel landed:
- No mature Go SDK existed for Max.
- The Max API is small enough (~10 endpoints used) that a custom client is well-bounded.
- Direct control over retry, timeouts, and request shape simplifies debugging (we hit three live-API discrepancies during development; see `docs/05-channels-messaging.md § 12 API Findings`).

### Streaming: answer-only (Опция 2)

`ReasoningStreamEnabled() = false`. Reasoning chunks from the agent loop are dropped; only answer chunks drive `stream.Update`. This matches the Slack channel's approach.

A future enhancement (Опция 3) could surface reasoning as a separate streaming message, similar to Telegram's lane system. The interface allows it without breaking changes.

### Placeholder ownership

`c.placeholders` (sync.Map) is the mid-flight handoff between `FinalizeStream` and `Send`:

1. `FinalizeStream` stores the mid of the streaming message.
2. `Send` calls `consumePlaceholder` (LoadAndDelete): if a mid is present, the first chunk is delivered via `editPlaceholder` (PUT /messages with markdown). If absent, normal POST.
3. After editing a finalized placeholder, the mid is **not** re-stored. Re-storing would cause the next `Send` into the same chat to overwrite the user-visible answer instead of producing a new message.

### Discriminator: `chat_type` not heuristics

Max API populates **both** `recipient.user_id` and `recipient.chat_id` for direct messages (one is the bot, the other is the dialog thread). The authoritative discriminator is `recipient.chat_type` ("dialog" vs "chat"). Code that infers from `user_id != 0` is wrong — see `Recipient.IsDialog()`.

## Live test commands

A standalone smoke test lives at `cmd/max-smoke/main.go` (not committed; copy from PR description if needed). Use it to verify a real bot end-to-end without spinning up the full goclaw stack:

```bash
MAX_BOT_TOKEN=$(cat ~/.poc-max-token) go run ./cmd/max-smoke
```

Send a message to the bot in Max — the smoke test echoes it back through the full Channel.Send pipeline including chunking, markdown, and (if streaming-aware) placeholder edits.

## Adding endpoints

When extending `client.go`:

1. Add the request/response types to `types.go` first. Match Max's actual JSON shape from a live PoC (see existing `testdata/*.json` for examples).
2. Add the method to `client.go` using `c.do(...)` for plumbing (handles base URL, auth header, query params, retry).
3. Add unit tests in `client_test.go` using `httptest.NewServer` with the same fixture pattern.
4. If the response shape differs from docs, document the discrepancy in `docs/05-channels-messaging.md § 12 API Findings`.

## Known limitations

- **Group bots not yet supported by Max platform.** Translator code paths are implemented and unit-tested, but live validation is blocked until Max permits adding bots to groups.
- **No outbound voice messages.** Inbound audio attachments are downloaded; outbound voice generation is not implemented.
- **No callback button responses.** `AnswerCallback` exists in the client; agent-loop integration is not wired.
- **No `request_contact` button setup.** `VerifyContactHash` validates incoming hashes; the outbound side (sending a request_contact button) is not implemented.

These are scoped extensions, all backward-compatible with the current interface.
