-- FK Integrity Audit Queries
-- Run these BEFORE applying migrations 000081 and 000082 to verify
-- the database has no orphans or tenant_id drift that would block the constraints.
-- All queries should return 0 rows on a clean database.

-- ============================================================
-- MIGRATION 081 PRE-CHECKS
-- ============================================================

-- hooks: tenant_id orphans (no matching tenant)
SELECT 'hooks.tenant_id orphans' AS check_name, COUNT(*) AS violations
FROM hooks h
LEFT JOIN tenants t ON t.id = h.tenant_id
WHERE h.tenant_id != '0193a5b0-7000-7000-8000-000000000001'  -- exclude master sentinel
  AND t.id IS NULL;

-- webhooks: tenant_id orphans
SELECT 'webhooks.tenant_id orphans' AS check_name, COUNT(*) AS violations
FROM webhooks w
LEFT JOIN tenants t ON t.id = w.tenant_id
WHERE t.id IS NULL;

-- webhook_calls: tenant_id orphans
SELECT 'webhook_calls.tenant_id orphans' AS check_name, COUNT(*) AS violations
FROM webhook_calls wc
LEFT JOIN tenants t ON t.id = wc.tenant_id
WHERE t.id IS NULL;

-- webhook_calls: agent_id orphans
SELECT 'webhook_calls.agent_id orphans' AS check_name, COUNT(*) AS violations
FROM webhook_calls wc
LEFT JOIN agents a ON a.id = wc.agent_id
WHERE wc.agent_id IS NOT NULL AND a.id IS NULL;

-- spans: trace_id orphans (NOT NULL column — any mismatch is critical)
SELECT 'spans.trace_id orphans' AS check_name, COUNT(*) AS violations
FROM spans s
LEFT JOIN traces t ON t.id = s.trace_id
WHERE t.id IS NULL;

-- spans: agent_id orphans
SELECT 'spans.agent_id orphans' AS check_name, COUNT(*) AS violations
FROM spans s
LEFT JOIN agents a ON a.id = s.agent_id
WHERE s.agent_id IS NOT NULL AND a.id IS NULL;

-- traces: agent_id orphans
SELECT 'traces.agent_id orphans' AS check_name, COUNT(*) AS violations
FROM traces t
LEFT JOIN agents a ON a.id = t.agent_id
WHERE t.agent_id IS NOT NULL AND a.id IS NULL;

-- usage_events: team_id orphans
SELECT 'usage_events.team_id orphans' AS check_name, COUNT(*) AS violations
FROM usage_events ue
LEFT JOIN agent_teams at ON at.id = ue.team_id
WHERE ue.team_id IS NOT NULL AND at.id IS NULL;

-- ============================================================
-- MIGRATION 082 PRE-CHECKS
-- ============================================================

-- webhooks: channel_id orphans
SELECT 'webhooks.channel_id orphans' AS check_name, COUNT(*) AS violations
FROM webhooks w
LEFT JOIN channel_instances ci ON ci.id = w.channel_id
WHERE w.channel_id IS NOT NULL AND ci.id IS NULL;

-- channel_contacts: merged_id self-ref orphans
SELECT 'channel_contacts.merged_id orphans' AS check_name, COUNT(*) AS violations
FROM channel_contacts cc
LEFT JOIN channel_contacts cc2 ON cc2.id = cc.merged_id
WHERE cc.merged_id IS NOT NULL AND cc2.id IS NULL;

-- traces: parent_trace_id self-ref orphans
SELECT 'traces.parent_trace_id orphans' AS check_name, COUNT(*) AS violations
FROM traces t
LEFT JOIN traces t2 ON t2.id = t.parent_trace_id
WHERE t.parent_trace_id IS NOT NULL AND t2.id IS NULL;

-- spans: parent_span_id self-ref orphans
SELECT 'spans.parent_span_id orphans' AS check_name, COUNT(*) AS violations
FROM spans s
LEFT JOIN spans s2 ON s2.id = s.parent_span_id
WHERE s.parent_span_id IS NOT NULL AND s2.id IS NULL;

-- subagent_tasks: spawned_by self-ref orphans
SELECT 'subagent_tasks.spawned_by orphans' AS check_name, COUNT(*) AS violations
FROM subagent_tasks st
LEFT JOIN subagent_tasks st2 ON st2.id = st.spawned_by
WHERE st.spawned_by IS NOT NULL AND st2.id IS NULL;

-- ============================================================
-- TENANT_ID DRIFT CHECKS
-- ============================================================

-- agent_context_files: tenant_id differs from parent agent
SELECT 'agent_context_files tenant_id drift' AS check_name, COUNT(*) AS violations
FROM agent_context_files acf
JOIN agents a ON a.id = acf.agent_id
WHERE acf.tenant_id != a.tenant_id;

-- user_context_files: tenant_id differs from parent agent
SELECT 'user_context_files tenant_id drift' AS check_name, COUNT(*) AS violations
FROM user_context_files ucf
JOIN agents a ON a.id = ucf.agent_id
WHERE ucf.tenant_id != a.tenant_id;

-- ============================================================
-- SUMMARY (run all checks at once)
-- ============================================================
-- Copy all SELECT statements above into a single query using UNION ALL
-- to get a single result set showing all violations.
