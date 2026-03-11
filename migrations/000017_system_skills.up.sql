ALTER TABLE skills ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_skills_system ON skills(is_system) WHERE is_system = true;
