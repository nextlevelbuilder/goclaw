-- Connected agents: external (Claude Code, Aider, …) or other AOS agents wired
-- into an agent at creation time as specialists it can delegate to. Stored as a
-- JSON array of config.ConnectedAgentSpec. NULL / absent = no connected agents
-- (backwards compatible with every pre-existing row).
ALTER TABLE agents ADD COLUMN IF NOT EXISTS connected_agents JSONB;
