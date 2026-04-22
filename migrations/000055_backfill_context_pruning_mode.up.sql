-- Backfill mode: "cache-ttl" for agents that have custom context_pruning config
-- but are missing the "mode" field. This preserves user intent after the
-- opt-in default flip (faithful port of TS behavior) — see CHANGELOG.
--
-- Rows matched: context_pruning is a non-empty JSON object without a "mode" key.
-- Rows skipped: NULL, empty object, non-object values, or already has "mode".
UPDATE agents
SET context_pruning = jsonb_set(context_pruning, '{mode}', '"cache-ttl"'::jsonb)
WHERE context_pruning IS NOT NULL
  AND jsonb_typeof(context_pruning) = 'object'
  AND context_pruning <> '{}'::jsonb
  AND NOT (context_pruning ? 'mode');
