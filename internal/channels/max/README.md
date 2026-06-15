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
- **Concurrent runs in the same chat may interleave placeholder handoffs.** When two agent runs are simultaneously active for one chat ID, both call `CreateStream` and post their own placeholder. The placeholder→Send handoff via `c.placeholders` is keyed only on chat ID, so the second `FinalizeStream` overwrites the first — the first run's `Send` then accidentally edits the second run's placeholder, while the second run's `Send` falls through to a fresh message. In production this is practically unreachable: goclaw's debounce coalesces rapid messages from one user, and per-session run limits cap concurrent runs at 1 in DM. Once Max enables group bots and concurrent group runs, the placeholder map will need to be keyed per-`RunContext` to fix this. Test `TestStreaming_ConcurrentRuns_DoNotInterfere` documents the current behaviour so future regressions are caught.

These are scoped extensions, all backward-compatible with the current interface.

## Webhook security

Max API does not provide a shared-secret signature scheme for webhook authenticity. The webhook URL is the only auth — operators MUST treat it as a secret credential.

**Required:**

- **Hard-to-guess URL.** Embed a UUID or random hex token in the path: `https://your.gateway/max/webhook/<uuid>`. Rotate on suspected leak (logs, error reports, screenshots).
- **TLS.** Webhook URL MUST be HTTPS. The channel enforces this at config-validation time; deployment must provision a real certificate.
- **Production policy.** Configure `dm_policy: "allowlist"` and populate `allow_from` with known user IDs, or use `dm_policy: "pairing"` to require an approval handshake. Both are enforced in `policy.go` before the agent loop is invoked. Note that `sender.user_id` from polling is server-attested by Max; webhook flows additionally verify HMAC-SHA256 in `auth.go` (see *Webhook security* below).

**Recommended:**

- **Ingress allowlist.** If Max API publishes static origin IPs, restrict ingress to those CIDRs at the load balancer / Cloud.ru ingress.
- **Rate limiting.** Cap requests per source IP at the gateway level. Webhook deliveries from Max are infrequent; a low ceiling (e.g. 10 rps) limits damage from URL leaks.
- **Monitor for unexpected `update_type`.** Log unrecognized update types at WARN; spikes suggest probing.
