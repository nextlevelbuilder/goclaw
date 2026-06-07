DELETE FROM skill_agent_grants sag
USING skills s, agents a
WHERE sag.skill_id = s.id
  AND sag.agent_id = a.id
  AND (
    sag.tenant_id <> a.tenant_id
    OR (s.is_system = false AND sag.tenant_id <> s.tenant_id)
  );
