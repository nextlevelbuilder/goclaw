DROP INDEX IF EXISTS idx_skills_system;
ALTER TABLE skills DROP COLUMN IF EXISTS is_system;
