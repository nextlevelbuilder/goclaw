ALTER TABLE skills ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE skills ADD COLUMN deps JSONB NOT NULL DEFAULT '{}';
ALTER TABLE skills ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT true;
CREATE INDEX idx_skills_system ON skills(is_system) WHERE is_system = true;
CREATE INDEX idx_skills_enabled ON skills(enabled) WHERE enabled = false;
