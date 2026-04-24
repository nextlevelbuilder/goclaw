-- Add chat_id to vault_documents for cross-chat isolation within isolated teams.
-- NULL = team-wide doc (shared mode or legacy); non-NULL = scoped to specific chat.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS chat_id TEXT;

-- Composite index for team + chat filtering (primary query pattern for isolated teams).
CREATE INDEX IF NOT EXISTS idx_vault_docs_team_chat
    ON vault_documents(team_id, chat_id)
    WHERE team_id IS NOT NULL;

-- -----------------------------------------------------------------------------
-- Backfill 1: team-scoped docs (scope='team', team_id set).
-- Two path layouts:
--   master tenant:     teams/<team_uuid>/<chat>/...
--   non-master tenant: tenants/<slug>/teams/<team_uuid>/<chat>/...
-- Chat segments starting with '.' (e.g. '.goclaw') are config dirs, not real chats — skip.
-- -----------------------------------------------------------------------------
UPDATE vault_documents vd
SET chat_id = (regexp_match(vd.path, '^(?:tenants/[^/]+/)?teams/[^/]+/([^/]+)/'))[1]
FROM agent_teams t
WHERE vd.team_id = t.id
  AND (t.settings->>'workspace_scope' IS NULL OR t.settings->>'workspace_scope' != 'shared')
  AND vd.path ~ '^(?:tenants/[^/]+/)?teams/[^/]+/[^.][^/]*/';

-- -----------------------------------------------------------------------------
-- Backfill 2: legacy docs from before team scope (team_id IS NULL) with chat
-- identifiers embedded in their path. Without chat_id these leak across chats
-- in isolated-team search because the `searchChatFilter` predicate cannot
-- distinguish them.
--
-- Subsystem vocabulary — any channel integration or delivery surface the
-- gateway writes under. Must match the channel names used as path segments
-- in internal/channels/* and workspace resolver (v2 + v3 layouts).
--   Channels:  telegram | discord | zalo | feishu | lark | whatsapp | slack |
--              line | messenger | wechat | viber
--   Transports: ws (browser / WS direct) | api (HTTP) | delegate (subagent)
--
-- Path layouts handled (in order of COALESCE priority):
--   <subsystem>/group_<anything>_<chat>/...              (Telegram-style group prefix)
--   <subsystem>/<chat>/...                               (bare legacy)
--   <agent_key>/<subsystem>/group_<anything>_<chat>/...  (agent-owned group)
--   <agent_key>/<subsystem>/<chat>/...                   (agent-owned direct)
--   tenants/<slug>/<subsystem>/<chat>/...                (non-master tenant)
--   <agent_key>/<botname>/group_<botname>_<chat>/...     (legacy bot channel)
--   <agent_key>/<botname>/<chat>/...                     (bot + numeric/ws chat)
--
-- Chat IDs: numeric (Telegram/Discord/Zalo), oc_xxx (Feishu/Lark), sanitized
-- JID (WhatsApp: "123_c_us"), `system`, user handles, UUIDs. Sanitizer
-- (workspace_resolver.go) replaces everything outside [a-zA-Z0-9_-] with `_`,
-- so the captured character class matches what's actually on disk.
--
-- Only populate when chat_id IS NULL so interceptor-stamped values survive.
-- -----------------------------------------------------------------------------
UPDATE vault_documents
SET chat_id = COALESCE(
    (regexp_match(path, '^(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber)/group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/([a-zA-Z0-9_-]+)/'))[1],
    (regexp_match(path, '^[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber)/group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/([a-zA-Z0-9_-]+)/'))[1],
    (regexp_match(path, '^tenants/[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/([a-zA-Z0-9_-]+)/'))[1],
    (regexp_match(path, '^group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^[^/]+/[^/]+/group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^[^/]+/[^/]+/([a-zA-Z0-9_-]+)/'))[1]
)
WHERE chat_id IS NULL
  AND team_id IS NULL
  AND (
    path ~ '^(?:[^/]+/)?(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/[^/]+/'
    OR path ~ '^tenants/[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/[^/]+/'
    OR path ~ '^group_[^/]+_-?[0-9]+/'
    OR path ~ '^[^/]+/[^/]+/(group_[^/]+_-?[0-9]+|[0-9]+)/'
  );
