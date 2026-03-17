ALTER TABLE sessions ADD COLUMN IF NOT EXISTS numeric_id BIGSERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_numeric_id ON sessions(numeric_id);
