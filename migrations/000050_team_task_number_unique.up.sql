-- Fix task_number uniqueness: scope per team_id only (not per chat_id).
-- Before this migration, task_number was scoped per (team_id, chat_id),
-- which allowed duplicate numbers across different chats in the same team.

-- Step 1: Deduplicate any existing duplicates by reassigning task_numbers globally per team.
-- Uses a window function to resequence tasks within each team by creation time.
DO $$
BEGIN
    -- Renumber all tasks per team using dense_rank ordered by created_at.
    -- This preserves relative ordering while eliminating cross-chat duplicates.
    UPDATE team_tasks tt
    SET task_number = ranked.new_number,
        identifier  = REGEXP_REPLACE(tt.identifier, '^T-\d+-', 'T-' || LPAD(ranked.new_number::text, 3, '0') || '-')
    FROM (
        SELECT id,
               DENSE_RANK() OVER (PARTITION BY team_id ORDER BY created_at, id) AS new_number
        FROM team_tasks
    ) ranked
    WHERE tt.id = ranked.id
      AND tt.task_number <> ranked.new_number;
END $$;

-- Step 2: Add unique constraint to enforce per-team uniqueness at DB level.
ALTER TABLE team_tasks
    ADD CONSTRAINT uq_team_tasks_team_number UNIQUE (team_id, task_number);
