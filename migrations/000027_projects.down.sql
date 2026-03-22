-- 000020_projects.down.sql
DROP TRIGGER IF EXISTS set_project_mcp_overrides_updated_at ON project_mcp_overrides;
DROP TRIGGER IF EXISTS set_projects_updated_at ON projects;
DROP FUNCTION IF EXISTS update_projects_updated_at();
DROP TABLE IF EXISTS project_mcp_overrides;
DROP TABLE IF EXISTS projects;
