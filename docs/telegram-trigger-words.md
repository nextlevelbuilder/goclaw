# Telegram trigger words (wake an agent by name)

In group chats a Telegram bot normally only reacts when it is `@mentioned`,
addressed with a `/command@bot`, or replied to. **Trigger words** let an agent
also wake up when a message names it by an alias — without an explicit mention.

Trigger words are a property of the **agent**, not the channel, so they travel
with the agent across every channel it serves. They are declared in the agent's
`IDENTITY.md` context file.

## How to configure

Add a `Trigger words:` line to the agent's `IDENTITY.md` (comma-separated):

```markdown
# IDENTITY.md — Who Am I?

- **Name:** Rex
- **Trigger words:** Alice, Boss, Chief
- **Creature:** an AI assistant that keeps a team's chats in order
- **Purpose:** answer questions and run tasks for the team
- **Emoji:** 🤖
```

The plain `Key: Value` form works too:

```markdown
Name: Rex
Trigger words: Alice, Boss, Chief
Emoji: 🤖
```

With the config above, in a group the bot wakes on messages like
`Alice, what's the status?` or `hey boss` — no `@mention` required. It keeps
ignoring unrelated chatter.

Edits to `IDENTITY.md` take effect within ~60s (the channel caches the parsed
list per agent); no restart needed.

## Matching rules

- **Whole word, case-insensitive.** `Boss` matches `boss` and `BOSS`, and
  matches even with surrounding punctuation (`boss!`, `hey, boss`). It does
  **not** match substrings — `bosses` or `bossy` will not trigger.
- **Unicode-aware.** Matching tokenizes on Unicode letters/digits rather than an
  ASCII `\b`, so aliases in any script (Cyrillic, CJK, accented Latin, …) match
  as whole words.
- Both the message text and a media **caption** are checked.

## Requirements

- **Groups only.** DMs already respond to every message, so trigger words only
  affect group (and channel) chats.
- **Disable the bot's Group Privacy in BotFather** (`/mybots` → Bot Settings →
  Group Privacy → Turn off), then re-add the bot to the group — otherwise
  Telegram never delivers plain (non-mention) group messages to the bot, and the
  gate has nothing to evaluate. Making the bot a group admin has the same effect.
- The group's pairing/policy gate still applies: a trigger word is treated like
  an `@mention`, so an unpaired group under `group_policy: pairing` still gets a
  pairing prompt rather than an answer.

## Implementation

- `bootstrap.ParseTriggerWords` extracts the list from `IDENTITY.md`.
- `internal/channels/telegram/wake_words.go` normalizes the list and does the
  whole-word, Unicode-aware match.
- The channel loads the agent's list via `GetAgentContextFiles` (tenant-scoped)
  and caches it per agent; the group gate in `handlers.go` treats a match as a
  mention.
