ALTER TABLE custom_tools DROP COLUMN IF EXISTS metadata;
DROP INDEX IF EXISTS idx_builtin_tools_category;
DROP TABLE IF EXISTS builtin_tools;
