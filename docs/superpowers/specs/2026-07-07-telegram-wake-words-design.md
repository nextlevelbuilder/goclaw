# Telegram wake-words + image_generation nil-guard fix

Date: 2026-07-07
Status: Approved

## Problem

Two items bundled into one image rebuild:

1. **Wake-words.** In Telegram group chats the bot only wakes on `@username`,
   `/cmd@bot`, or a reply to its own message (`detectMention`). The user wants
   it to also wake when a message names it by an alias — e.g. "Паша", "Слышь",
   "Чмо" — without an explicit @-mention. DMs already respond to every message,
   so this only affects groups.

2. **Latent nil-pointer panic.** `imageGenToolDef =
   providers.ToolDefinition{Type: "image_generation"}` has `Function == nil`.
   It is appended for codex-provider agents with image-gen enabled, then the
   `mcpDefs` counting loop in `makeBuildFilteredTools`
   (`internal/agent/loop_pipeline_callbacks.go`) dereferences `td.Function.Name`
   with no nil check → SIGSEGV on every message. Currently worked around per-bot
   with `other_config.allow_image_generation=false`. Fixing the guard lets us
   re-enable native image generation.

## Requirements

- Groups only. DM behavior unchanged.
- Whole-word match, case-insensitive, Unicode/Cyrillic-aware word boundaries.
  "чмо" matches "Чмо, привет" but NOT "чмоки". "Паша" does not match "Пашка".
- Word list is configurable per channel instance (not hardcoded).
- Match against both message text and media caption.
- nil-guard makes the `mcpDefs` loop panic-safe; re-enable image-gen for the bot.

## Design

### Wake-words (`internal/channels/telegram/`)

1. `factory.go` — add `WakeWords []string \`json:"wake_words,omitempty"\`` to
   `telegramInstanceConfig`; pass into `tgCfg`.
2. `internal/config/config_channels.go` — add `WakeWords []string
   \`json:"wake_words,omitempty"\`` to `TelegramConfig`.
3. `channel.go` — store a normalized (lowercased) `map[string]struct{}` of
   wake-words on the channel, built once at construction.
4. `handlers_utils.go` — new `matchesWakeWord(msg *telego.Message) bool`:
   - Returns false fast if the set is empty.
   - Tokenizes `msg.Text` and `msg.Caption` by runs of
     `unicode.IsLetter || unicode.IsDigit` (Cyrillic-safe; NOT regexp `\b`).
   - Lowercases each token and checks membership in the set.
5. `handlers.go` (~line 293, after the reply-implicit-mention block, before the
   yield block): `if !wasMentioned && c.matchesWakeWord(message) { wasMentioned = true }`.
   Sits inside the existing `isGroup && (requireMention || yield)` gate, so DMs
   and non-require-mention groups are unaffected.

Config values are single tokens matched as whole words; multi-word phrases are
out of scope (YAGNI).

### nil-guard (`internal/agent/loop_pipeline_callbacks.go`)

In the `mcpDefs` counting loop, guard before dereferencing:

```go
for _, td := range toolDefs {
    if td.Function == nil {
        continue
    }
    if strings.HasPrefix(strings.TrimSpace(td.Function.Name), "mcp_") {
        mcpDefs++
    }
}
```

## Testing

- Unit test for `matchesWakeWord`:
  - true: "Паша", "ПАША", "паша," , "эй, чмо!", wake-word in caption.
  - false: "чмоки", "Пашка", empty text, latin-only, empty word list.
- Unit test asserting a `toolDefs` slice containing a Function-nil entry does
  not panic in the mcpDefs path (or a focused regression on the loop).

## Deployment

- `docker compose build goclaw && docker compose ... up -d goclaw` (compose has a
  `build:` section pointing at local source + Dockerfile).
- Set the bot's config: `channel_instances.config` += `"wake_words":
  ["Паша","Слышь","Чмо"]` (SQL).
- Re-enable image-gen: remove/flip `other_config.allow_image_generation` on
  `bot-pavel-valerievich` back to default (or `true`).

## Surface parity (CLAUDE.md)

- Gateway/channel: primary change (telegram factory, channel, handler, detect).
- API/config contract: `wake_words` added to config structs.
- Web UI: **deferred** — field stays settable via channel config JSON/API; the
  UI control + i18n (en/vi/zh) is a follow-up, not this iteration.
- CLI: N/A — channel configured via DB/API.

## Out of scope

- Multi-word wake phrases.
- Wake-words for other channels (Discord/Feishu/Zalo/WhatsApp).
- Web UI control for wake_words (deferred follow-up).
