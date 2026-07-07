# Agent trigger-words + image_generation nil-guard fix

Date: 2026-07-07
Status: Approved (revised — agent-MD-driven, not channel config)

## Problem

Two items bundled into one image rebuild:

1. **Trigger words.** In Telegram group chats the bot only wakes on `@username`,
   `/cmd@bot`, or a reply to its own message (`detectMention`). The user wants
   it to also wake when a message names it by an alias — e.g. "Паша", "Слышь",
   "Чмо" — without an explicit @-mention. DMs already respond to every message,
   so this only affects groups.

   **Ownership: the agent, not the channel.** There are many agents and many
   channels, and one agent may serve several channels. Trigger words are an
   identity property of the agent and must be declared in the agent's own MD
   context files, so they travel with the agent across every channel it serves.
   A per-channel config field is the wrong home and is rejected.

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
- Trigger words are declared **per agent, in the agent's `IDENTITY.md`** — not
  hardcoded and not in channel config.
- Match against both message text and media caption.
- Editing `IDENTITY.md` takes effect without a restart (short TTL cache).
- nil-guard makes the `mcpDefs` loop panic-safe; re-enable image-gen for the bot.

## Design

### Source of truth: `IDENTITY.md` (`Trigger words:` field)

Agents declare aliases in their agent-level `IDENTITY.md` context file, using the
existing `Key: Value` convention (`parseIdentityContent` style):

```
Trigger words: Паша, Слышь, Чмо
```

IDENTITY.md is the "who am I / what am I called" file — aliases are identity
facts (SOUL.md is behavior). Agent-level file → shared across all the agent's
channels. Comma-separated; single tokens matched as whole words (multi-word
phrases out of scope, YAGNI).

### Parser (`internal/bootstrap`)

`ParseTriggerWords(identityContent string) []string` — scans lines for a
`Trigger words` / `Trigger word` key (case-insensitive), tolerant of the
markdown bullet form `- **Trigger words:** …`, splits the value on commas,
trims, drops blanks.

### Matcher (`internal/channels/telegram/wake_words.go`) — source-agnostic

- `normalizeWakeWords([]string) map[string]struct{}` — lowercase/trim into a set.
- `textHasWakeWord(text string, set) bool` — tokenizes on runs of
  `unicode.IsLetter || unicode.IsDigit` (Cyrillic-safe, NOT regexp `\b`),
  lowercases each token, checks set membership.

These stay reusable so other channels can adopt the same gate later.

### Channel wiring (`internal/channels/telegram/`)

1. `channel.go` — per-agent trigger cache on the Channel (one channel_instance =
   one agent): `triggerWords map[string]struct{}`, `triggerWordsAt time.Time`,
   `triggerMu sync.Mutex`. TTL = 60s.
2. `agentTriggerWords(ctx)` — if cache fresh, return it; else
   `resolveAgentUUID` → `agentStore.GetAgentContextFiles` → find
   `bootstrap.IdentityFile` → `bootstrap.ParseTriggerWords` →
   `normalizeWakeWords` → cache. On any error, fail open (return current/empty
   set — never wake spuriously, never crash the handler).
3. `matchesTriggerWords(ctx, msg)` — `textHasWakeWord` over `msg.Text` and
   `msg.Caption` using the cached set.
4. `handlers.go` (after the reply-implicit-mention block, before the yield
   block): `if !wasMentioned && c.matchesTriggerWords(ctx, message) { wasMentioned = true }`.
   Inside the existing `isGroup && (requireMention || yield)` gate → DMs and
   non-require-mention groups unaffected.

No channel-config field, no `TelegramConfig`/factory change for trigger words.

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

- `ParseTriggerWords`: `Trigger words: Паша, Слышь, Чмо` → 3; bullet form
  `- **Trigger words:** …`; case-insensitive key; missing key → empty; blanks
  dropped.
- `textHasWakeWord` / `normalizeWakeWords`:
  - true: "Паша", "ПАША", "паша," , "эй, чмо!", trigger in caption.
  - false: "чмоки", "Пашка", empty text, latin-only, empty word set.
- `matchesTriggerWords` on a Channel with a stubbed cached set (text + caption).
- nil-guard: a `toolDefs` slice containing a Function-nil entry does not panic in
  the mcpDefs path.

## Deployment

- `docker compose build goclaw && docker compose ... up -d goclaw` (compose has a
  `build:` section pointing at local source + Dockerfile).
- Add to `bot-pavel-valerievich` IDENTITY.md (agent_context_files):
  `Trigger words: Паша, Слышь, Чмо`.
- Re-enable image-gen: remove/flip `other_config.allow_image_generation` on
  `bot-pavel-valerievich` back to default (or `true`).

## Surface parity (CLAUDE.md)

- Gateway/channel: primary change (telegram channel + handler + bootstrap parser).
- API/config contract: no new DB/config fields — reads existing `IDENTITY.md`
  context file.
- Web UI: trigger words are edited as normal IDENTITY.md content via the existing
  agent context-file editor. No new control.
- CLI: N/A.

## Out of scope

- Multi-word trigger phrases.
- Trigger words for other channels (Discord/Feishu/Zalo/WhatsApp) — same
  reusable matcher/parser; wire per-channel in a follow-up.
