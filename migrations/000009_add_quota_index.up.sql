-- Partial index for quota checker: efficiently counts top-level traces per user in time windows.
-- Eliminates parent_trace_id IS NULL post-filter (89% of traces are top-level).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_traces_quota
ON traces (user_id, created_at DESC)
WHERE parent_trace_id IS NULL AND user_id IS NOT NULL;
