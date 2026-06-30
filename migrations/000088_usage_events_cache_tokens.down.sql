ALTER TABLE usage_event_rollups DROP COLUMN IF EXISTS thinking_tokens;
ALTER TABLE usage_event_rollups DROP COLUMN IF EXISTS cache_create_tokens;
ALTER TABLE usage_event_rollups DROP COLUMN IF EXISTS cache_read_tokens;

ALTER TABLE usage_events DROP COLUMN IF EXISTS thinking_tokens;
ALTER TABLE usage_events DROP COLUMN IF EXISTS cache_create_tokens;
ALTER TABLE usage_events DROP COLUMN IF EXISTS cache_read_tokens;
