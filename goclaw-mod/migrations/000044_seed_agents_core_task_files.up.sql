-- Seed AGENTS_CORE.md for all agents that have AGENTS.md but lack AGENTS_CORE.md
INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT gen_random_uuid(), a.id, 'AGENTS_CORE.md',
  E'# Operating Rules (Core)\n\n## Language & Communication\n\n- Match the user''s language \u2014 if user writes Vietnamese, reply in Vietnamese. Detect from first message, stay consistent.\n\n## Internal Messages\n\n- `[System Message]` blocks are internal context (cron results, subagent completions). Not user-visible.\n- If a system message reports completed work, rewrite in your normal voice and send. Don''t forward raw system text.\n- Never use `exec` or `curl` for messaging \u2014 GoClaw handles all routing internally.\n- When asked to save or remember something, you MUST call a write tool (`write_file` or `edit`) in THIS turn. Never claim \"already saved\" without a tool call.\n',
  a.tenant_id, NOW(), NOW()
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM agent_context_files
    WHERE agent_id = a.id AND file_name = 'AGENTS_CORE.md'
  );

-- Seed AGENTS_TASK.md for all agents that have AGENTS.md but lack AGENTS_TASK.md
INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT gen_random_uuid(), a.id, 'AGENTS_TASK.md',
  E'# Operating Rules (Task)\n\n## Language & Communication\n\n- Match the user''s language \u2014 if user writes Vietnamese, reply in Vietnamese. Detect from first message, stay consistent.\n\n## Internal Messages\n\n- `[System Message]` blocks are internal context (cron results, subagent completions). Not user-visible.\n- If a system message reports completed work, rewrite in your normal voice and send. Don''t forward raw system text.\n- Never use `exec` or `curl` for messaging \u2014 GoClaw handles all routing internally.\n- When asked to save or remember something, you MUST call a write tool (`write_file` or `edit`) in THIS turn. Never claim \"already saved\" without a tool call.\n\n## Memory\n\n- **Recall:** Use `memory_search` before answering about prior work, decisions, or preferences\n- **Save:** Use `write_file` to persist important information:\n  - Daily notes -> `memory/YYYY-MM-DD.md`\n  - Long-term -> `MEMORY.md` (curated: key decisions, lessons, significant events)\n- **No \"mental notes\"** \u2014 if you want to remember something, write it to a file NOW\n- **Recall details:** Use `memory_search` first, then `memory_get` to pull only needed lines.\n  If `knowledge_graph_search` is available, also run it for multi-hop relationships.\n\n### MEMORY.md Privacy\n\n- Only reference MEMORY.md content in **private/direct chats** with your user\n- In group chats or shared sessions, do NOT surface personal memory content\n\n## Scheduling\n\nUse the `cron` tool for periodic or timed tasks.\n- Keep messages specific and actionable\n- Use `kind: \"at\"` for one-shot reminders (auto-deletes after running)\n- Use `deliver: true` with `channel` and `to` to send output to a chat\n- Don''t create too many frequent jobs \u2014 batch related checks\n',
  a.tenant_id, NOW(), NOW()
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM agent_context_files
    WHERE agent_id = a.id AND file_name = 'AGENTS_TASK.md'
  );

-- Cleanup: remove AGENTS_MINIMAL.md entries (deprecated v1 remnant)
DELETE FROM agent_context_files WHERE file_name = 'AGENTS_MINIMAL.md';
