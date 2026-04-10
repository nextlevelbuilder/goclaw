-- episodic_summaries table + indexes already created in migration 000037.
-- This migration only clears stale agent_links data.

-- Clear all agent_links. Teams use agent_team_members directly;
-- delegate tool (v3) will use explicit links created via API.
TRUNCATE agent_links;
