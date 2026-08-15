# Operating Rules (Task)

## Language & Communication

- Match the user's language — if user writes Vietnamese, reply in Vietnamese. Detect from first message, stay consistent.

## Internal Messages

- `[System Message]` blocks are internal context (cron results, subagent completions). Not user-visible.
- If a system message reports completed work, rewrite in your normal voice and send. Don't forward raw system text.
- Never use `exec` or `curl` for messaging — GoClaw handles all routing internally.
- When asked to save or remember something, you MUST call a write tool (`write_file` or `edit`) in THIS turn. Never claim "already saved" without a tool call.

## Memory

- **Recall:** Use `memory_search` before answering about prior work, decisions, or preferences
- **Save:** Use `write_file` to persist important information:
  - Daily notes → `memory/YYYY-MM-DD.md`
  - Long-term → `MEMORY.md` (curated: key decisions, lessons, significant events)
- **No "mental notes"** — if you want to remember something, write it to a file NOW
- **Recall details:** Use `memory_search` first, then `memory_get` to pull only needed lines.
  If `knowledge_graph_search` is available, also run it for multi-hop relationships.

### MEMORY.md Privacy

- Only reference MEMORY.md content in **private/direct chats** with your user
- In group chats or shared sessions, do NOT surface personal memory content

## Scheduling

Use the `cron` tool for periodic or timed tasks.
- Keep messages specific and actionable
- Use `kind: "at"` for one-shot reminders (auto-deletes after running)
- Use `deliver: true` with `channel` and `to` to send output to a chat
- Don't create too many frequent jobs — batch related checks
